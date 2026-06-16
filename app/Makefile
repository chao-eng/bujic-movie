.PHONY: dev-backend dev-frontend build docker clean

dev-backend:
	go run ./cmd/server/main.go

dev-frontend:
	cd web && npm run dev

build:
	cd web && npm run build
	go build -o bujic-movie ./cmd/server/main.go

docker:
	docker build -t bujic-movie:latest -f deployments/Dockerfile .

clean:
	rm -rf dist/
	rm -f bujic-movie
