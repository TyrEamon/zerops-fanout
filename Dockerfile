# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS builder

WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags "-s -w -X main.version=zerops-docker" -o /out/fanout .

FROM alpine:3.20

RUN apk add --no-cache \
  bash \
  ca-certificates \
  curl \
  iproute2 \
  iptables \
  openvpn \
  && mkdir -p /var/lib/fanout /usr/local/bin

COPY --from=builder /out/fanout /usr/local/bin/fanout
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN chmod +x /usr/local/bin/fanout /usr/local/bin/docker-entrypoint.sh

ENV WEB_PORT=8899
ENV WORK_DIR=/var/lib/fanout
ENV MAX_SLOTS=20

EXPOSE 8899
EXPOSE 20000-20019

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
