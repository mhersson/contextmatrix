.PHONY: build run test test-race fmt lint build-frontend test-frontend lint-frontend install-frontend install docker-build clean test-integration history

VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD)
BUILD_TIME ?= $(shell date -u "+%Y-%m-%d %H:%M")
GIT_COMMIT_FULL ?= $(shell git rev-parse HEAD 2>/dev/null)
BUILD_TIME_RFC3339 ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILDINFO_PKG := github.com/mhersson/contextmatrix/internal/buildinfo
HISTORY_FILE := internal/buildinfo/history/merges.log
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.gitCommit=$(GIT_COMMIT) -X 'main.buildTime=$(BUILD_TIME)' -X $(BUILDINFO_PKG).version=$(VERSION) -X $(BUILDINFO_PKG).commit=$(GIT_COMMIT_FULL) -X $(BUILDINFO_PKG).buildTime=$(BUILD_TIME_RFC3339)

build: build-frontend history
	go build -trimpath -ldflags="$(LDFLAGS)" -o contextmatrix ./cmd/contextmatrix

run: build
	./contextmatrix

# The stub keeps //go:embed all:dist compilable on fresh clones (web/dist is
# gitignored and only exists after a frontend build); nothing is committed,
# and real builds overwrite dist via build-frontend.
#
# No -count=1: Go's cache keys on package inputs, so an edited package always
# re-runs. The stateful targets (test-race, test-integration) carry it where a
# cached pass would be wrong.
test:
	@mkdir -p web/dist && touch web/dist/.gitkeep
	go test ./cmd/... ./internal/...

test-race:
	CGO_ENABLED=1 go test -race -count=1 ./internal/chat/... ./internal/api/... ./internal/mcp/... ./internal/githubauth/...

fmt:
	gofumpt -w ./cmd ./internal ./test ./web/embed.go

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

install: build-frontend history
	go install -ldflags="$(LDFLAGS)" ./cmd/contextmatrix

# The arguments equal changelog.GitLogArgs; TestHistoryCommands_MatchGitLogArgs
# keeps the two in step. A checkout without git records no history and still builds.
history:
	git log --no-show-signature --first-parent --merges -n 100 '--format=%cI%x1f%s%x1f%b%x1e' > $(HISTORY_FILE) 2>/dev/null || true

docker-build: history
	docker build \
		--build-arg VERSION="$(VERSION)" \
		--build-arg GIT_COMMIT="$(GIT_COMMIT)" \
		--build-arg BUILD_TIME="$(BUILD_TIME)" \
		--build-arg GIT_COMMIT_FULL="$(GIT_COMMIT_FULL)" \
		--build-arg BUILD_TIME_RFC3339="$(BUILD_TIME_RFC3339)" \
		-t contextmatrix .

clean:
	rm -f contextmatrix
	rm -rf web/dist

test-integration:
	go test -tags=integration -count=1 -timeout 20m ./test/integration/...
