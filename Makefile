.PHONY: build run test test-race fmt lint build-frontend test-frontend lint-frontend install-frontend install install-config docker-build clean test-integration test-integration-real

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
	docker build \
		--build-arg VERSION="$(VERSION)" \
		--build-arg GIT_COMMIT="$(GIT_COMMIT)" \
		--build-arg BUILD_TIME="$(BUILD_TIME)" \
		-t contextmatrix .

install-config:
	scripts/install.sh

clean:
	rm -f contextmatrix
	rm -rf web/dist

test-integration:
	go test -tags=integration -count=1 -timeout 15m ./test/integration/...

# 60m: stub suite ~25s + image build ~5s + autonomous-real (≤20m internal) +
# hitl-real (≤30m internal) + slack. Without this, go's default 10m timeout
# panics mid-run and t.Cleanup never fires, so cards.json / run.md / cm.log
# are never written and the operator can't diagnose what wedged.
test-integration-real:
	CM_REAL_CLAUDE=1 go test -tags=integration -count=1 -timeout 60m ./test/integration/...
