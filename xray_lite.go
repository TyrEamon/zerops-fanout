package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	liteXrayListen = "127.0.0.1"
	liteXrayPort   = 10000
	liteXrayPath   = "/vless"
)

type LiteOutbound struct {
	Mode string `json:"mode"`
	Port int    `json:"port,omitempty"`
}

type LiteXray struct {
	mu         sync.Mutex
	workDir    string
	configPath string
	statePath  string
	uuidPath   string
	logPath    string
	uuid       string
	outbound   LiteOutbound
	cmd        *exec.Cmd
	done       chan error
	lastErr    string
}

func NewLiteXray(workDir string) (*LiteXray, error) {
	dir := filepath.Join(workDir, "xray")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	x := &LiteXray{
		workDir:    dir,
		configPath: filepath.Join(dir, "config.json"),
		statePath:  filepath.Join(dir, "outbound.json"),
		uuidPath:   filepath.Join(dir, "uuid"),
		logPath:    filepath.Join(dir, "xray.log"),
		outbound:   LiteOutbound{Mode: "direct"},
	}
	if err := x.loadUUID(); err != nil {
		return nil, err
	}
	if err := x.loadOutbound(); err != nil {
		return nil, err
	}
	return x, nil
}

func (x *LiteXray) loadUUID() error {
	if v := strings.TrimSpace(os.Getenv("XRAY_UUID")); v != "" {
		x.uuid = v
		return os.WriteFile(x.uuidPath, []byte(v+"\n"), 0600)
	}
	if b, err := os.ReadFile(x.uuidPath); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			x.uuid = v
			return nil
		}
	}
	v, err := randomUUID()
	if err != nil {
		return err
	}
	x.uuid = v
	return os.WriteFile(x.uuidPath, []byte(v+"\n"), 0600)
}

func (x *LiteXray) loadOutbound() error {
	b, err := os.ReadFile(x.statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var state LiteOutbound
	if err := json.Unmarshal(b, &state); err != nil {
		return err
	}
	if state.Mode == "socks" {
		x.outbound = state
	} else {
		x.outbound = LiteOutbound{Mode: "direct"}
	}
	return nil
}

func (x *LiteXray) Start() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.restartLocked()
}

func (x *LiteXray) Stop() {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.stopLocked()
}

func (x *LiteXray) SetOutbound(mode string, port int) error {
	x.mu.Lock()
	defer x.mu.Unlock()

	if mode != "socks" {
		x.outbound = LiteOutbound{Mode: "direct"}
	} else {
		x.outbound = LiteOutbound{Mode: "socks", Port: port}
	}
	b, err := json.MarshalIndent(x.outbound, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(x.statePath, append(b, '\n'), 0600); err != nil {
		return err
	}
	return x.restartLocked()
}

func (x *LiteXray) Status(r *http.Request) map[string]any {
	x.mu.Lock()
	defer x.mu.Unlock()

	return map[string]any{
		"running": x.runningLocked(),
		"error":   x.lastErr,
		"uuid":    x.uuid,
		"path":    liteXrayPath,
		"listen":  liteXrayListen,
		"port":    liteXrayPort,
		"outbound": map[string]any{
			"mode": x.outbound.Mode,
			"port": x.outbound.Port,
		},
		"share": x.shareLinkLocked(r),
	}
}

func (x *LiteXray) restartLocked() error {
	x.stopLocked()
	if err := x.writeConfigLocked(); err != nil {
		x.lastErr = err.Error()
		return err
	}

	logFile, err := os.OpenFile(x.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		x.lastErr = err.Error()
		return err
	}
	cmd := exec.Command("xray", "run", "-config", x.configPath)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET=/usr/local/share/xray")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		x.lastErr = err.Error()
		return err
	}

	done := make(chan error, 1)
	x.cmd = cmd
	x.done = done
	x.lastErr = ""
	go func() {
		err := cmd.Wait()
		_ = logFile.Close()
		done <- err

		x.mu.Lock()
		if x.cmd == cmd {
			x.cmd = nil
			x.done = nil
			if err != nil {
				x.lastErr = err.Error()
			}
		}
		x.mu.Unlock()
	}()
	log.Printf("轻量 Xray 已启动：%s:%d%s -> %s", liteXrayListen, liteXrayPort, liteXrayPath, x.outboundLabelLocked())
	return nil
}

func (x *LiteXray) stopLocked() {
	if x.cmd == nil || x.cmd.Process == nil {
		x.cmd = nil
		x.done = nil
		return
	}
	_ = x.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-x.done:
	case <-time.After(2 * time.Second):
		_ = x.cmd.Process.Kill()
		select {
		case <-x.done:
		case <-time.After(1 * time.Second):
		}
	}
	x.cmd = nil
	x.done = nil
}

func (x *LiteXray) runningLocked() bool {
	return x.cmd != nil && x.cmd.Process != nil
}

func (x *LiteXray) outboundLabelLocked() string {
	if x.outbound.Mode == "socks" {
		return fmt.Sprintf("socks 127.0.0.1:%d", x.outbound.Port)
	}
	return "direct"
}

func (x *LiteXray) writeConfigLocked() error {
	outbounds := []any{}
	if x.outbound.Mode == "socks" {
		outbounds = append(outbounds, map[string]any{
			"tag":      "fanout",
			"protocol": "socks",
			"settings": map[string]any{
				"servers": []any{
					map[string]any{
						"address": "127.0.0.1",
						"port":    x.outbound.Port,
					},
				},
			},
		})
	}
	outbounds = append(outbounds,
		map[string]any{"tag": "direct", "protocol": "freedom"},
		map[string]any{"tag": "blocked", "protocol": "blackhole"},
	)

	config := map[string]any{
		"log": map[string]any{
			"loglevel": "warning",
		},
		"inbounds": []any{
			map[string]any{
				"tag":      "vless-ws",
				"listen":   liteXrayListen,
				"port":     liteXrayPort,
				"protocol": "vless",
				"settings": map[string]any{
					"clients": []any{
						map[string]any{
							"id":    x.uuid,
							"email": "default",
						},
					},
					"decryption": "none",
				},
				"streamSettings": map[string]any{
					"network": "ws",
					"wsSettings": map[string]any{
						"path": liteXrayPath,
					},
				},
				"sniffing": map[string]any{
					"enabled":      true,
					"destOverride": []string{"http", "tls", "quic"},
				},
			},
		},
		"outbounds": outbounds,
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{
				map[string]any{
					"type":        "field",
					"ip":          []string{"geoip:private"},
					"outboundTag": "blocked",
				},
			},
		},
	}

	b, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(x.configPath, append(b, '\n'), 0600)
}

func (x *LiteXray) shareLinkLocked(r *http.Request) string {
	host := publicHostFromEnv()
	if host == "" {
		host = firstHeader(r, "X-Forwarded-Host")
		if host == "" {
			host = r.Host
		}
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "" {
		host = "example.com"
	}

	proto := firstHeader(r, "X-Forwarded-Proto")
	port := "443"
	security := "tls"
	if proto == "http" {
		port = "80"
		security = "none"
	}

	values := url.Values{}
	values.Set("encryption", "none")
	values.Set("security", security)
	values.Set("type", "ws")
	values.Set("host", host)
	values.Set("path", liteXrayPath)
	return fmt.Sprintf("vless://%s@%s:%s?%s#%s",
		x.uuid,
		host,
		port,
		values.Encode(),
		url.QueryEscape("Zerops-fanout"),
	)
}

func publicHostFromEnv() string {
	for _, key := range []string{"PUBLIC_HOST", "ARGO_DOMAIN"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			v = strings.TrimSpace(strings.Split(v, ",")[0])
			v = strings.TrimPrefix(strings.TrimPrefix(v, "https://"), "http://")
			v = strings.Split(v, "/")[0]
			return v
		}
	}
	return ""
}

func firstHeader(r *http.Request, name string) string {
	v := r.Header.Get(name)
	if v == "" {
		return ""
	}
	return strings.TrimSpace(strings.Split(v, ",")[0])
}

func randomUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	), nil
}

func liteXrayProxy() http.Handler {
	target, _ := url.Parse(fmt.Sprintf("http://%s:%d", liteXrayListen, liteXrayPort))
	proxy := httputil.NewSingleHostReverseProxy(target)
	baseDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		baseDirector(r)
		r.Host = target.Host
	}
	return proxy
}

func apiLiteStatus(x *LiteXray) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, x.Status(r))
	}
}

func apiLiteOutbound(x *LiteXray, m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mode := r.URL.Query().Get("mode")
		if mode != "socks" {
			if err := x.SetOutbound("direct", 0); err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, x.Status(r))
			return
		}

		port, err := strconv.Atoi(r.URL.Query().Get("port"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "port 参数无效"})
			return
		}
		found := false
		for _, t := range m.Tunnels() {
			if t.Status == "up" && t.Port == port {
				found = true
				break
			}
		}
		if !found {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "这个 SOCKS5 端口当前没有可用隧道"})
			return
		}
		if err := x.SetOutbound("socks", port); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, x.Status(r))
	}
}
