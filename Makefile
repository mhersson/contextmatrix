.PHONY: build run test test-race fmt lint build-frontend test-frontend lint-frontend install-frontend install install-config docker-build clean

VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD)
BUILD_TIME ?= $(shell date -u "+%Y-%m-%d %H:%M")
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.gitCommit=$(GIT_COMMIT) -X 'main.buildTime=$(BUILD_TIME)'

build: build-frontend
	go build -trimpath -ldflags="$(LDFLAGS)" -o contextmatrix ./cmd/contextmatrix

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
	go install -ldflags="$(LDFLAGS)" ./cmd/contextmatrix

docker-build:
	docker build -t contextmatrix .

install-config:
	scripts/install.sh

clean:
	rm -f contextmatrix
	rm -rf web/dist
