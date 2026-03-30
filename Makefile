.PHONY: build run test fmt lint build-frontend install-frontend

build:
	go build -o contextmatrix ./cmd/contextmatrix

run: build
	./contextmatrix

test:
	go test ./...

fmt:
	go fmt ./...

lint:
	golangci-lint run

install-frontend:
	cd web && npm install

build-frontend:
	cd web && npm run build
