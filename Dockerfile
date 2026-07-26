# syntax=docker/dockerfile:1

# 1. Build the web UI.
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 2. Build the Go binary with the UI embedded.
#    CGO_ENABLED=0 works because the SQLite driver is pure Go, which is what
#    makes the runtime image this small and cross-compilation trivial.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/geetproject ./cmd/geetproject

# 3. Runtime. Alpine rather than scratch so the entrypoint can fix ownership of
#    the appdata volume and drop privileges, which is what Unraid expects.
FROM alpine:3.22
RUN apk add --no-cache su-exec tzdata wget
COPY --from=build /out/geetproject /usr/local/bin/geetproject
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

ENV GEETPROJECT_DB=/data/geet.db \
    GEETPROJECT_ADDR=:8080 \
    PUID=99 \
    PGID=100
VOLUME /data
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["geetproject", "serve"]
