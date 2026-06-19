# ===== Phase 1: Frontend Build =====
FROM --platform=$BUILDPLATFORM node:20-alpine AS frontend-builder
WORKDIR /app/web
COPY web/package*.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci
COPY web/ .
RUN npm run build

# ===== Phase 2: Go Build =====
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY pkg/ ./pkg/
COPY embed.go ./
COPY --from=frontend-builder /app/dist ./dist

ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o bujic-movie ./cmd/server

# ===== Phase 2.5: Static ffprobe Downloader =====
FROM --platform=$BUILDPLATFORM alpine:3.19 AS ffprobe-downloader
RUN apk add --no-cache curl tar xz
ARG TARGETARCH
RUN set -ex; \
    if [ "${TARGETARCH}" = "amd64" ]; then \
        URL="https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz"; \
    elif [ "${TARGETARCH}" = "arm64" ]; then \
        URL="https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-arm64-static.tar.xz"; \
    else \
        URL="https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz"; \
    fi; \
    curl -L "$URL" -o ffmpeg.tar.xz; \
    mkdir ffmpeg-static; \
    tar -xJf ffmpeg.tar.xz -C ffmpeg-static --strip-components=1; \
    mv ffmpeg-static/ffprobe /usr/local/bin/ffprobe; \
    chmod +x /usr/local/bin/ffprobe; \
    rm -rf ffmpeg.tar.xz ffmpeg-static;

# ===== Phase 3: Runner =====
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend-builder /app/bujic-movie .
COPY --from=ffprobe-downloader /usr/local/bin/ffprobe /usr/local/bin/ffprobe

ENV BUJIC_SERVER_MODE=release

EXPOSE 8080
VOLUME ["/media", "/downloads", "/app/data"]
ENTRYPOINT ["./bujic-movie"]



