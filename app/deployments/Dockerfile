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

# ===== Phase 3: Runner =====
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend-builder /app/bujic-movie .

EXPOSE 8080
VOLUME ["/media", "/downloads", "/app/data"]
ENTRYPOINT ["./bujic-movie"]


