#!/bin/sh
set -eu

LOG=/var/www/fanout-start.log

{
  echo "=== fanout start $(date) ==="
  id
  pwd
  ls -la /var/www
  docker ps -a || true
  docker volume create fanout-data
  docker rm -f fanout 2>/dev/null || true

  echo "creating fanout container"
  docker create --name fanout \
    --privileged \
    --network=host \
    -v fanout-data:/var/lib/fanout \
    -v /var/www:/app:ro \
    -e WEB_PORT=8899 \
    -e WORK_DIR=/var/lib/fanout \
    -e MAX_SLOTS=20 \
    -e FANOUT_PASSWORD="${FANOUT_PASSWORD:-}" \
    -e FANOUT_BASEPATH="${FANOUT_BASEPATH:-}" \
    -e XRAY_UUID="${XRAY_UUID:-}" \
    -e ARGO_ENABLED="${ARGO_ENABLED:-}" \
    -e ARGO_DOMAIN="${ARGO_DOMAIN:-}" \
    -e ARGO_AUTH="${ARGO_AUTH:-}" \
    -e ARGO_TUNNEL_ID="${ARGO_TUNNEL_ID:-}" \
    -e ARGO_PORT="${ARGO_PORT:-}" \
    -e PUBLIC_HOST="${PUBLIC_HOST:-}" \
    alpine:3.20 sh -eux -c 'id; uname -a; ls -la /app; apk add --no-cache bash ca-certificates curl iproute2 iptables openvpn unzip; mkdir -p /tmp/xray /usr/local/share/xray; curl -fsSL -o /tmp/xray.zip https://github.com/XTLS/Xray-core/releases/latest/download/Xray-linux-64.zip; unzip -q /tmp/xray.zip -d /tmp/xray; install -m 755 /tmp/xray/xray /usr/local/bin/xray; cp /tmp/xray/geoip.dat /tmp/xray/geosite.dat /usr/local/share/xray/ 2>/dev/null || true; rm -rf /tmp/xray /tmp/xray.zip; curl -fsSL -o /usr/local/bin/cloudflared https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64; chmod +x /usr/local/bin/cloudflared; cp /app/fanout /usr/local/bin/fanout; cp /app/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh; chmod +x /usr/local/bin/fanout /usr/local/bin/docker-entrypoint.sh; /usr/local/bin/fanout -version; xray version | head -1; cloudflared --version; exec /usr/local/bin/docker-entrypoint.sh'
  docker start fanout

  docker ps -a
} 2>&1 | tee "$LOG"

while true; do
  docker logs --tail 200 fanout 2>&1 || true
  sleep 30
done
