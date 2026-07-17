//go:build integration

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
	harnessRoot string
	cmBinary    string
	tmpRoot     string
)

// workerLabels are the docker labels the agent and chat executors stamp on
// their worker containers. The orphan sweep removes leftovers from crashed
// runs by these labels.
var workerLabels = []string{
	"contextmatrix.agent=true",
	"contextmatrix.chat=true",
}

// TestMain builds ONLY the CM binary up front - the sibling agent/chat repos
// are built lazily by ensureAgentAssets/ensureChatAssets so backend-free tests
// (TestMultiUserAdminSurface, TestChatREST, TestSmoke) run with no sibling
// checkout present. Docker worker images are likewise built on demand.
func TestMain(m *testing.M) {
	if err := setup(); err != nil {
		log.Fatalf("integration harness setup failed: %v", err)
	}

	code := m.Run()
	os.Exit(code)
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

	ctx := context.Background()

	if err := buildCM(ctx, cmBinary); err != nil {
		return fmt.Errorf("build CM: %w", err)
	}

	if err := sweepOrphans(ctx); err != nil {
		log.Printf("orphan sweep failed (non-fatal): %v", err)
	}

	log.Printf("harness ready: cm=%s tmp=%s", cmBinary, tmpRoot)

	return nil
}

func buildCM(ctx context.Context, out string) error {
	repoRoot := filepath.Join(harnessRoot, "..", "..")

	// CM's binary embeds web/dist via embed.FS, so the frontend must exist
	// before `go build`. Invoke `make build-frontend` (not `npm run build`
	// directly) to honour any future frontend build-pipeline changes.
	if _, err := os.Stat(filepath.Join(repoRoot, "web", "dist", "index.html")); err != nil {
		if _, err := os.Stat(filepath.Join(repoRoot, "web", "node_modules")); err != nil {
			install := exec.CommandContext(ctx, "make", "install-frontend")
			install.Dir = repoRoot
			install.Stdout, install.Stderr = os.Stderr, os.Stderr

			if err := install.Run(); err != nil {
				return fmt.Errorf("make install-frontend: %w", err)
			}
		}

		front := exec.CommandContext(ctx, "make", "build-frontend")
		front.Dir = repoRoot
		front.Stdout, front.Stderr = os.Stderr, os.Stderr

		if err := front.Run(); err != nil {
			return fmt.Errorf("make build-frontend: %w", err)
		}
	}

	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, "./cmd/contextmatrix")
	cmd.Dir = repoRoot
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr

	return cmd.Run()
}

// sweepOrphans removes worker containers left over from prior crashed runs,
// identified by the agent/chat executor labels.
func sweepOrphans(ctx context.Context) error {
	var ids []string

	for _, label := range workerLabels {
		out, err := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "label="+label).Output()
		if err != nil {
			return fmt.Errorf("docker ps (label=%s): %w", label, err)
		}

		ids = append(ids, nonEmptyLines(string(out))...)
	}

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

	for line := range strings.SplitSeq(s, "\n") {
		if line != "" {
			out = append(out, line)
		}
	}

	return out
}

// dockerListByLabel returns container IDs matching a single docker label
// selector (e.g. "contextmatrix.agent=true").
func dockerListByLabel(label string) []string {
	out, err := exec.Command("docker", "ps", "-aq", "--filter", "label="+label).Output()
	if err != nil {
		return nil
	}

	return nonEmptyLines(string(out))
}
