# fanout on Zerops Docker

This repository packages [byJoey/fanout](https://github.com/byJoey/fanout) for Zerops Docker service.

It does not build a Docker image on your local computer. Zerops runs `docker build` remotely during deployment.

## Zerops

Create or reuse a Docker service and connect this repository.

Use setup name:

```text
fanout
```

The service runs fanout in a privileged Docker container with host networking.

## Ports

- Web UI: `8899`
- SOCKS5 pool: `20000-20019`

The upstream fanout source normally picks random SOCKS5 ports from `20000-60000`.
This fork narrows the pool to `20000-20019`, so Zerops can expose predictable TCP ports.

## After Deploy

Open the Zerops terminal and read the generated path and password:

```bash
docker exec fanout sh -c 'echo basepath=$(cat /var/lib/fanout/basepath); echo password=$(cat /var/lib/fanout/password)'
docker logs fanout --tail 80
```

Open:

```text
https://<your-zerops-web-domain>/
```

or the generated base path shown in the logs.

After adding a VPN Gate node in the fanout UI, it will show the SOCKS5 port.
Use Zerops public access / port settings to expose the matching TCP port if needed.

## Notes

- Requires Zerops Docker service, not Node.js/Golang runtime service.
- Requires privileged Docker, TUN, netns, and iptables support.
- VPN Gate nodes are public volunteer nodes, so availability and speed vary.
