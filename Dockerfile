# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
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

COPY --from=build /out/proposarr /usr/local/bin/proposarr
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh

# Runs as an unprivileged user; override with `docker run --user` / compose
# `user:` to match the owner of the directory mounted at /config.
RUN mkdir -p /config && chown 1000:1000 /config && chmod 0775 /config
USER 1000:1000
ENV HOME=/config/.home \
    PROPOSARR_DATA_DIR=/config/data
WORKDIR /config
VOLUME /config

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]
# Until the web UI lands the container idles; run commands with
# docker exec -it proposarr proposarr run --kind movies --add
CMD ["sleep", "infinity"]
