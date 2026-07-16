//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Sibling repositories, resolved relative to this repo's worktree root. Both
// are built as-is from their protocol-v0.8.0-compatible checkouts. The worker
// image tags they produce live in config_test.go (initBoardsRepo bakes them
// into the board's remote_execution.worker_image).
const (
	agentRepoRel = "contextmatrix-agent"
	chatRepoRel  = "contextmatrix-chat"
)

// Minimal worker Dockerfiles. The production docker/Dockerfile.worker bakes in
// the full language toolchain (Go/Node/Python/Rust); the smoke harness scripts
// a trivial change and a `true` verify gate, so all a worker needs is the
// statically-linked binary plus git + certificates. Mirrors the production
// ENTRYPOINT and the UID 1000 / workspace layout each worker expects.
const agentWorkerDockerfile = `FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      bash ca-certificates git \
    && rm -rf /var/lib/apt/lists/*
COPY contextmatrix-agent /usr/local/bin/contextmatrix-agent
RUN useradd -m -u 1000 -s /bin/bash user && mkdir -p /home/user/workspace && chown 1000:1000 /home/user/workspace
USER user
WORKDIR /home/user
ENTRYPOINT ["contextmatrix-agent", "work"]
`

const chatWorkerDockerfile = `FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      bash ca-certificates git \
    && rm -rf /var/lib/apt/lists/*
COPY contextmatrix-chat /usr/local/bin/contextmatrix-chat
RUN useradd -m -u 1000 -s /bin/bash user && mkdir -p /workspace && chown 1000:1000 /workspace
USER user
WORKDIR /home/user
ENTRYPOINT ["contextmatrix-chat", "work"]
`

// agentBuilt/chatBuilt hold the built host serve binary path for a sibling
// backend. The worker image is built and tagged (agentWorkerImage /
// chatWorkerImage) as a side effect.
type siblingAssets struct {
	hostBinary string // built serve binary, run on the host
}

var (
	agentOnce   sync.Once
	agentBuilt  *siblingAssets
	agentErr    error
	agentAbsent bool

	chatOnce   sync.Once
	chatBuilt  *siblingAssets
	chatErr    error
	chatAbsent bool
)

// ensureAgentAssets lazily builds the contextmatrix-agent host binary and the
// minimal cm-agent-worker:test image. Skips the calling test (t.Skip) when the
// sibling repo is absent, so multiuser/chat-REST runs need no sibling checkout.
func ensureAgentAssets(t *testing.T) *siblingAssets {
	t.Helper()

	agentOnce.Do(func() {
		repo := siblingRepo(agentRepoRel)
		if _, err := os.Stat(repo); err != nil {
			agentAbsent = true

			return
		}

		agentBuilt, agentErr = buildSiblingAssets(
			repo, "contextmatrix-agent", agentWorkerImage, agentWorkerDockerfile)
	})

	if agentAbsent {
		t.Skipf("sibling repo %s not present at %s — skipping", agentRepoRel, siblingRepo(agentRepoRel))
	}

	if agentErr != nil {
		t.Fatalf("build agent assets: %v", agentErr)
	}

	return agentBuilt
}

// ensureChatAssets mirrors ensureAgentAssets for contextmatrix-chat.
func ensureChatAssets(t *testing.T) *siblingAssets {
	t.Helper()

	chatOnce.Do(func() {
		repo := siblingRepo(chatRepoRel)
		if _, err := os.Stat(repo); err != nil {
			chatAbsent = true

			return
		}

		chatBuilt, chatErr = buildSiblingAssets(
			repo, "contextmatrix-chat", chatWorkerImage, chatWorkerDockerfile)
	})

	if chatAbsent {
		t.Skipf("sibling repo %s not present at %s — skipping", chatRepoRel, siblingRepo(chatRepoRel))
	}

	if chatErr != nil {
		t.Fatalf("build chat assets: %v", chatErr)
	}

	return chatBuilt
}

// siblingRepo resolves a sibling repository path by walking up from
// harnessRoot until an ancestor directory contains a child named `name`. This
// finds .../Development/<name> whether the harness runs from the main checkout
// (.../contextmatrix/test/integration) or a worktree
// (.../contextmatrix/.worktrees/<branch>/test/integration), where the sibling
// is a different number of levels up.
func siblingRepo(name string) string {
	dir := harnessRoot

	for range 8 {
		candidate := filepath.Join(dir, name)
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			return candidate
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}

		dir = parent
	}

	// Fall back to the conventional …/Development/<name> so the skip message
	// points somewhere sensible when the sibling is genuinely absent.
	return filepath.Join(harnessRoot, "..", "..", "..", "..", "..", name)
}

// buildSiblingAssets compiles the sibling's single binary twice: once as a
// host serve binary (default toolchain) and once statically for the worker
// image (CGO_ENABLED=0). It then docker-builds a minimal image from dockerfile
// with the static binary as its only payload.
func buildSiblingAssets(repo, binaryName, imageTag, dockerfile string) (*siblingAssets, error) {
	ctx := context.Background()

	pkg := "./cmd/" + binaryName

	hostBinary := filepath.Join(tmpRoot, binaryName+"-host")
	if err := goBuild(ctx, repo, pkg, hostBinary, nil); err != nil {
		return nil, fmt.Errorf("build host binary: %w", err)
	}

	// Static linux binary for the container image, built into a throwaway
	// docker build context alongside the generated Dockerfile.
	buildCtx, err := os.MkdirTemp(tmpRoot, "dockerctx-"+binaryName+"-")
	if err != nil {
		return nil, fmt.Errorf("mktemp docker context: %w", err)
	}

	staticBinary := filepath.Join(buildCtx, binaryName)
	if err := goBuild(ctx, repo, pkg, staticBinary, []string{
		"CGO_ENABLED=0", "GOOS=linux",
	}); err != nil {
		return nil, fmt.Errorf("build static worker binary: %w", err)
	}

	if err := os.WriteFile(filepath.Join(buildCtx, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return nil, fmt.Errorf("write Dockerfile: %w", err)
	}

	// --network=host runs the RUN step (apt-get) in the host network namespace,
	// avoiding the bridge veth setup — which some kernels/daemons lack (see
	// requireContainerNetworking) — while still giving the build internet.
	build := exec.CommandContext(ctx, "docker", "build", "--network=host", "-t", imageTag, buildCtx)
	build.Stdout, build.Stderr = os.Stderr, os.Stderr

	if err := build.Run(); err != nil {
		return nil, fmt.Errorf("docker build %s: %w", imageTag, err)
	}

	return &siblingAssets{hostBinary: hostBinary}, nil
}

// requireContainerNetworking skips the calling test when the Docker daemon
// cannot launch a container on the default bridge network. The agent and chat
// executors launch every worker on the bridge with an --add-host host-gateway
// mapping (internal/executor/hosts.go) and expose no host-network knob, so a
// daemon that cannot set up bridge networking simply cannot run the worker.
// This is outside the harness's control, so it is a skip with an actionable
// reason — not a failure. The fix is host/daemon-level (a common cause is a
// missing veth kernel module, which may need a reboot after a kernel upgrade
// removed the running kernel's module tree).
func requireContainerNetworking(t *testing.T) {
	t.Helper()

	out, err := exec.Command("docker", "run", "--rm", "debian:bookworm-slim", "true").CombinedOutput()
	if err == nil {
		return
	}

	s := string(out)
	if strings.Contains(s, "failed to set up container networking") ||
		strings.Contains(s, "operation not supported") ||
		strings.Contains(s, "failed to create endpoint") {
		t.Skipf("bridge container networking is unavailable, so worker containers cannot launch: %s\n"+
			"The agent and chat executors launch every worker on the default bridge with no host-network "+
			"knob, so this must be fixed at the host/daemon level before these scenarios can run.", firstLine(s))
	}

	t.Fatalf("docker bridge networking probe failed unexpectedly: %v\n%s", err, s)
}

// firstLine returns the first non-empty line of s, trimmed.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if l := strings.TrimSpace(line); l != "" {
			return l
		}
	}

	return strings.TrimSpace(s)
}

// goBuild runs `go build -o out pkg` in dir with optional extra environment.
func goBuild(ctx context.Context, dir, pkg, out string, extraEnv []string) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", out, pkg)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr

	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}

	return cmd.Run()
}
