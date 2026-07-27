#!/bin/sh
set -eu

mkdir -p "${WORK_DIR}" /dev/net

if [ ! -e /dev/net/tun ]; then
  mknod /dev/net/tun c 10 200 2>/dev/null || true
fi
chmod 600 /dev/net/tun 2>/dev/null || true

sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1 || true

if [ -n "${FANOUT_PASSWORD:-}" ]; then
  printf '%s\n' "${FANOUT_PASSWORD}" > "${WORK_DIR}/password"
fi

if [ -n "${FANOUT_BASEPATH:-}" ]; then
  case "${FANOUT_BASEPATH}" in
    /*) printf '%s\n' "${FANOUT_BASEPATH}" > "${WORK_DIR}/basepath" ;;
    *) printf '/%s\n' "${FANOUT_BASEPATH}" > "${WORK_DIR}/basepath" ;;
  esac
fi

start_cloudflared() {
  if [ "${ARGO_ENABLED:-}" != "true" ] && [ -z "${ARGO_AUTH:-}" ]; then
    return 0
  fi

  argo_port="${ARGO_PORT:-${WEB_PORT}}"
  argo_dir="${WORK_DIR}/cloudflared"
  argo_domain="$(printf '%s' "${ARGO_DOMAIN:-}" | sed 's#^https://##; s#^http://##; s#/.*$##')"
  mkdir -p "${argo_dir}"

  echo "cloudflared starting"
  echo "argo port: ${argo_port}"
  if [ -n "${argo_domain}" ]; then
    echo "argo domain: ${argo_domain}"
  fi

  if [ -n "${ARGO_AUTH:-}" ]; then
    if printf '%s' "${ARGO_AUTH}" | grep -q "TunnelSecret"; then
      if [ -z "${argo_domain}" ]; then
        echo "ARGO_DOMAIN is required when ARGO_AUTH is a credentials JSON"
        return 0
      fi
      printf '%s' "${ARGO_AUTH}" > "${argo_dir}/tunnel.json"
      tunnel_id="${ARGO_TUNNEL_ID:-}"
      if [ -z "${tunnel_id}" ]; then
        tunnel_id="$(printf '%s' "${ARGO_AUTH}" | sed -n 's/.*"TunnelID"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
      fi
      cat > "${argo_dir}/config.yml" <<EOF
tunnel: ${tunnel_id}
credentials-file: ${argo_dir}/tunnel.json
protocol: http2
ingress:
  - hostname: ${argo_domain}
    service: http://localhost:${argo_port}
  - service: http_status:404
EOF
      cloudflared tunnel --edge-ip-version auto --no-autoupdate --config "${argo_dir}/config.yml" run >"${argo_dir}/cloudflared.log" 2>&1 &
    else
      cloudflared tunnel --edge-ip-version auto --no-autoupdate --protocol http2 run --token "${ARGO_AUTH}" >"${argo_dir}/cloudflared.log" 2>&1 &
    fi
  else
    cloudflared tunnel --edge-ip-version auto --no-autoupdate --protocol http2 --url "http://localhost:${argo_port}" --logfile "${argo_dir}/cloudflared.log" --loglevel info >/dev/null 2>&1 &
  fi
}

start_cloudflared

echo "fanout starting"
echo "web: ${WEB_PORT}"
echo "work dir: ${WORK_DIR}"
echo "max slots: ${MAX_SLOTS}"
echo "socks ports: 20000-20019"
if [ -n "${FANOUT_BASEPATH:-}" ]; then
  echo "base path: ${FANOUT_BASEPATH}"
fi

exec /usr/local/bin/fanout \
  -web "${WEB_PORT}" \
  -dir "${WORK_DIR}" \
  -max "${MAX_SLOTS}"
