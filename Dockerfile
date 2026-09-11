# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /web
RUN npm install -g pnpm@11
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/proposarr ./cmd/proposarr

FROM ubuntu:24.04
ARG CLAUDE_CODE_VERSION=latest
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl gnupg tini tzdata \
 && curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
 && apt-get install -y --no-install-recommends nodejs \
 && npm install -g @anthropic-ai/claude-code@${CLAUDE_CODE_VERSION} \
 && npm cache clean --force \
 && apt-get purge -y gnupg && apt-get autoremove -y \
 && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/proposarr /usr/local/bin/proposarr-bin
COPY docker/proposarr.sh /usr/local/bin/proposarr
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod 0755 /usr/local/bin/proposarr /usr/local/bin/entrypoint.sh && mkdir -p /config

# The entrypoint starts as root only to hand /config to PUID:PGID, then
# Proposarr and the claude CLI run as that user (99:100 on Unraid).
ENV HOME=/config/.home \
    PROPOSARR_DATA_DIR=/config/data \
    PROPOSARR_LISTEN=:8585 \
    PUID=1000 \
    PGID=1000
WORKDIR /config
VOLUME /config
EXPOSE 8585

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD curl -fsS http://127.0.0.1:8585/healthz || exit 1

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]
CMD ["serve"]
