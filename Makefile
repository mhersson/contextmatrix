.PHONY: build run test fmt lint build-frontend install-frontend

build: build-frontend
	go build -o contextmatrix ./cmd/contextmatrix

run: build
	./contextmatrix

test:
	go test ./cmd/... ./internal/...

fmt:
	go fmt ./...

lint:
	golangci-lint run

install-frontend:
	cd web && npm install

build-frontend:
	cd web && npm run build
