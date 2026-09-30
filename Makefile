.PHONY: all web build test run dev clean

all: build

web:
	cd web && npm ci && npm run build

build: web
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/viceroy ./cmd/viceroy
	CGO_ENABLED=0 go build -o bin/fakesimplefin ./cmd/fakesimplefin

test:
	go vet ./...
	go test ./...
	cd web && npm run typecheck

# Backend on :8420 using ./viceroy.toml; run `make dev-web` in another terminal for hot reload.
run: build
	./bin/viceroy serve

dev-web:
	cd web && npm run dev

sqlc:
	go tool sqlc generate

clean:
	rm -rf bin web/dist/assets web/dist/*.js web/dist/*.html web/dist/*.webmanifest web/dist/*.svg
