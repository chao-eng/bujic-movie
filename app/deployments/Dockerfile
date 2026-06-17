# ===== Phase 1: Frontend Build =====
FROM node:20-alpine AS frontend-builder
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# ===== Phase 2: Go Build =====
FROM golang:1.26-alpine AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend-builder /app/dist ./dist
RUN CGO_ENABLED=0 GOOS=linux go build -o bujic-movie ./cmd/server

# ===== Phase 3: Runner =====
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend-builder /app/bujic-movie .

EXPOSE 8080
VOLUME ["/media", "/downloads", "/app/data"]
ENTRYPOINT ["./bujic-movie"]
