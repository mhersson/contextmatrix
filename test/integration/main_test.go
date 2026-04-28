//go:build integration

// Package integration_test runs end-to-end harness tests of CM + runner
// + a stub or real worker image. Gated behind the `integration` build
// tag so `make test` ignores it. See
// docs/superpowers/specs/2026-04-28-self-hosted-integration-harness-design.md.
package integration_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var (
	harnessRoot  string
	cmBinary     string
	runnerBinary string
	tmpRoot      string
	realClaudeOn bool
)

func TestMain(m *testing.M) {
	if err := setup(); err != nil {
		log.Fatalf("integration harness setup failed: %v", err)
	}
	os.Exit(m.Run())
}

func setup() error {
	abs, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve harness root: %w", err)
	}
	harnessRoot = abs

	tmpRoot, err = os.MkdirTemp("", fmt.Sprintf("cm-int-%d-", os.Getpid()))
	if err != nil {
		return fmt.Errorf("mktemp: %w", err)
	}

	cmBinary = filepath.Join(tmpRoot, "contextmatrix")
	runnerBinary = filepath.Join(tmpRoot, "contextmatrix-runner")

	realClaudeOn = os.Getenv("CM_REAL_CLAUDE") == "1"

	ctx := context.Background()

	if err := buildCM(ctx, cmBinary); err != nil {
		return fmt.Errorf("build CM: %w", err)
	}
	if err := buildRunner(ctx, runnerBinary); err != nil {
		return fmt.Errorf("build runner: %w", err)
	}
	if err := buildStubImage(ctx); err != nil {
		return fmt.Errorf("build stub image: %w", err)
	}
	if realClaudeOn {
		if err := ensureRealClaudeImage(ctx); err != nil {
			return fmt.Errorf("ensure real-Claude image: %w", err)
		}
	}
	if err := sweepOrphans(ctx); err != nil {
		log.Printf("orphan sweep failed (non-fatal): %v", err)
	}

	log.Printf("harness ready: cm=%s runner=%s tmp=%s realClaude=%v",
		cmBinary, runnerBinary, tmpRoot, realClaudeOn)
	return nil
}

func buildCM(ctx context.Context, out string) error {
	repoRoot := filepath.Join(harnessRoot, "..", "..")
	cmd := exec.CommandContext(ctx, "go", "build",
		"-o", out, "./cmd/contextmatrix")
	cmd.Dir = repoRoot
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}

func buildRunner(ctx context.Context, out string) error {
	runnerRepo := filepath.Join(harnessRoot, "..", "..", "..", "contextmatrix-runner")
	if _, err := os.Stat(runnerRepo); err != nil {
		return fmt.Errorf("runner repo not found at %s: %w", runnerRepo, err)
	}
	cmd := exec.CommandContext(ctx, "go", "build",
		"-o", out, "./cmd/contextmatrix-runner")
	cmd.Dir = runnerRepo
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}

func buildStubImage(ctx context.Context) error {
	if imageExists(ctx, "cm-stub-orchestrated:test") {
		return nil
	}
	stubDir := filepath.Join(harnessRoot, "stub-worker")
	cmd := exec.CommandContext(ctx, "docker", "build",
		"-t", "cm-stub-orchestrated:test", stubDir)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}

// ensureRealClaudeImage builds cm-orchestrated:test if missing by
// delegating to the runner repo's `make docker-orchestrated`.
func ensureRealClaudeImage(ctx context.Context) error {
	if imageExists(ctx, "cm-orchestrated:test") {
		return nil
	}
	runnerRepo := filepath.Join(harnessRoot, "..", "..", "..", "contextmatrix-runner")
	build := exec.CommandContext(ctx, "make", "docker-orchestrated")
	build.Dir = runnerRepo
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("make docker-orchestrated: %w", err)
	}
	tag := exec.CommandContext(ctx, "docker", "tag",
		"contextmatrix/agent:latest", "cm-orchestrated:test")
	tag.Stdout, tag.Stderr = os.Stderr, os.Stderr
	return tag.Run()
}

func imageExists(ctx context.Context, ref string) bool {
	return exec.CommandContext(ctx, "docker", "image", "inspect", ref).Run() == nil
}

// sweepOrphans removes containers from prior crashed runs (by label).
func sweepOrphans(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, "docker", "ps", "-aq",
		"--filter", "label=contextmatrix.test").Output()
	if err != nil {
		return fmt.Errorf("docker ps: %w", err)
	}
	ids := nonEmptyLines(string(out))
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"rm", "-f"}, ids...)
	rm := exec.CommandContext(ctx, "docker", args...)
	rm.Stdout, rm.Stderr = os.Stderr, os.Stderr
	return rm.Run()
}

// nonEmptyLines splits s on \n and drops empty strings.
func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
