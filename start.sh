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

  if docker ps -a --format '{{.Names}}' | grep -qx fanout; then
    echo "fanout container already exists, starting it"
    docker start fanout || true
  else
    echo "creating fanout container"
    docker create --name fanout \
      --privileged \
      --network=host \
      -v fanout-data:/var/lib/fanout \
      -v /var/www:/app:ro \
      -e WEB_PORT=8899 \
      -e WORK_DIR=/var/lib/fanout \
      -e MAX_SLOTS=20 \
      alpine:3.20 sh -eux -c 'id; uname -a; ls -la /app; apk add --no-cache bash ca-certificates curl iproute2 iptables openvpn; cp /app/fanout /usr/local/bin/fanout; cp /app/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh; chmod +x /usr/local/bin/fanout /usr/local/bin/docker-entrypoint.sh; /usr/local/bin/fanout -version; exec /usr/local/bin/docker-entrypoint.sh'
    docker start fanout
  fi

  docker ps -a
} 2>&1 | tee "$LOG"

while true; do
  docker logs --tail 200 fanout 2>&1 || true
  sleep 30
done
