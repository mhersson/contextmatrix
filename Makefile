.PHONY: build run test test-race fmt lint build-frontend test-frontend lint-frontend install-frontend install install-config docker-build clean

build: build-frontend
	go build -trimpath -ldflags="-s -w" -o contextmatrix ./cmd/contextmatrix

run: build
	./contextmatrix

test:
	go test ./cmd/... ./internal/...

test-race:
	CGO_ENABLED=1 go test -race ./cmd/... ./internal/...

fmt:
	go fmt ./...

lint:
	golangci-lint run

test-frontend:
	cd web && npx vitest run

lint-frontend:
	cd web && npm run lint

install-frontend:
	cd web && npm install

build-frontend:
	cd web && npm run build

install: build-frontend
	go install ./cmd/contextmatrix

docker-build:
	docker build -t contextmatrix .

install-config:
	scripts/install.sh

clean:
	rm -f contextmatrix
	rm -rf web/dist
