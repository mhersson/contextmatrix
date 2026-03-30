.PHONY: build run test fmt lint

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
