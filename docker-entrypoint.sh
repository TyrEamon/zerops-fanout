#!/bin/sh
set -eu

mkdir -p "${WORK_DIR}" /dev/net

if [ ! -e /dev/net/tun ]; then
  mknod /dev/net/tun c 10 200 2>/dev/null || true
fi
chmod 600 /dev/net/tun 2>/dev/null || true

sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1 || true

echo "fanout starting"
echo "web: ${WEB_PORT}"
echo "work dir: ${WORK_DIR}"
echo "max slots: ${MAX_SLOTS}"
echo "socks ports: 20000-20019"

exec /usr/local/bin/fanout \
  -web "${WEB_PORT}" \
  -dir "${WORK_DIR}" \
  -max "${MAX_SLOTS}"
