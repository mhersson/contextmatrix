//go:build integration

package integration_test

import (
	"fmt"
	"net"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitServer is a smart-HTTP git server backed by `git http-backend` run
// through net/http/cgi. It serves a single seeded bare repository at
// /work.git and accepts anonymous clone AND push (receive-pack) — the
// worker authenticates with a bearer token embedded in the clone URL, but
// the server ignores it (GIT_HTTP_EXPORT_ALL + http.receivepack=true).
//
// Bound to 0.0.0.0 so worker containers can reach it via
// host.docker.internal:<port>; the port is kernel-assigned.
type gitServer struct {
	srv     *http.Server
	rootDir string // GIT_PROJECT_ROOT — holds work.git
	workGit string // absolute path to the bare repo
	port    int
}

// startGitServer seeds a bare repo with a trivial Go project (README + go.mod
// + main.go), then stands up the smart-HTTP CGI handler. The returned server
// is torn down in t.Cleanup.
func startGitServer(t *testing.T, rl *runLog) *gitServer {
	t.Helper()

	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("git not found on PATH: %v", err)
	}

	backend := gitHTTPBackendPath(t)

	root := t.TempDir()
	workGit := filepath.Join(root, "work.git")

	seedBareRepo(t, gitBin, workGit)

	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("git server listen: %v", err)
	}

	port := ln.Addr().(*net.TCPAddr).Port

	handler := &cgi.Handler{
		Path: backend,
		Env: []string{
			"GIT_PROJECT_ROOT=" + root,
			"GIT_HTTP_EXPORT_ALL=1",
			// http-backend runs receive-pack only when the repo opts in;
			// seedBareRepo sets http.receivepack=true in the bare repo config.
		},
	}

	mux := http.NewServeMux()
	mux.Handle("/", handler)

	srv := &http.Server{Handler: mux}

	gs := &gitServer{srv: srv, rootDir: root, workGit: workGit, port: port}

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			if rl != nil {
				rl.writeLine("gitserver", "serve: "+err.Error())
			}
		}
	}()

	t.Cleanup(func() {
		_ = srv.Close()
	})

	if rl != nil {
		rl.writeLine("gitserver", fmt.Sprintf("serving %s on 0.0.0.0:%d", workGit, port))
	}

	return gs
}

// containerURL is the clone URL as seen from inside a worker container,
// reachable via the Docker host-gateway alias.
func (g *gitServer) containerURL() string {
	return fmt.Sprintf("http://host.docker.internal:%d/work.git", g.port)
}

// remoteBranches returns the branch refs present on the bare repo, as seen by
// `git ls-remote --heads` from the host (127.0.0.1). Used to assert the worker
// pushed its feature branch.
func (g *gitServer) remoteBranches(t *testing.T) []string {
	t.Helper()

	url := fmt.Sprintf("http://127.0.0.1:%d/work.git", g.port)

	out, err := exec.Command("git", "ls-remote", "--heads", url).CombinedOutput()
	if err != nil {
		t.Fatalf("git ls-remote %s: %v\n%s", url, err, out)
	}

	var branches []string

	for _, line := range nonEmptyLines(string(out)) {
		// Each line: "<sha>\trefs/heads/<branch>".
		const prefix = "refs/heads/"
		if idx := indexOf(line, prefix); idx >= 0 {
			branches = append(branches, line[idx+len(prefix):])
		}
	}

	return branches
}

// gitHTTPBackendPath locates the git-http-backend CGI executable. It lives
// outside PATH (in git's libexec dir), so we ask git where its exec-path is.
func gitHTTPBackendPath(t *testing.T) string {
	t.Helper()

	out, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Fatalf("git --exec-path: %v", err)
	}

	execPath := string(trimTrailingNewline(out))
	backend := filepath.Join(execPath, "git-http-backend")

	if _, err := os.Stat(backend); err != nil {
		t.Fatalf("git-http-backend not found at %s: %v", backend, err)
	}

	return backend
}

// seedBareRepo creates a bare repo at path and pushes an initial commit
// containing a trivial Go project. The commit is built in a throwaway
// working clone next to the bare repo.
func seedBareRepo(t *testing.T, gitBin, bareRepo string) {
	t.Helper()

	if err := os.MkdirAll(bareRepo, 0o755); err != nil {
		t.Fatalf("mkdir bare repo: %v", err)
	}

	mustRun(t, "", gitBin, "init", "--bare", "--initial-branch=main", bareRepo)
	// Allow anonymous push over HTTP for this repo.
	mustRun(t, bareRepo, gitBin, "config", "http.receivepack", "true")

	work := t.TempDir()
	mustRun(t, work, gitBin, "init", "--initial-branch=main")
	mustRun(t, work, gitBin, "config", "user.email", "seed@cm.test")
	mustRun(t, work, gitBin, "config", "user.name", "seed")

	files := map[string]string{
		"README.md": "# work\n\nSeed repository for the integration harness.\n",
		"go.mod":    "module example.com/work\n\ngo 1.26\n",
		"main.go":   "package main\n\nfunc main() {}\n",
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(work, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write seed file %s: %v", name, err)
		}
	}

	mustRun(t, work, gitBin, "add", ".")
	mustRun(t, work, gitBin, "commit", "-m", "seed work repo")
	mustRun(t, work, gitBin, "remote", "add", "origin", bareRepo)
	mustRun(t, work, gitBin, "push", "origin", "main")
}

// indexOf returns the first index of sub in s, or -1.
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}

	return -1
}

// trimTrailingNewline drops a single trailing "\n" (and "\r") from b.
func trimTrailingNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}

	return b
}
