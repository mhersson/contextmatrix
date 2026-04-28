//go:build integration

package integration_test

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// process wraps a subprocess + its captured stderr buffer.
type process struct {
	cmd    *exec.Cmd
	stderr *threadSafeBuffer
}

// threadSafeBuffer guards a bytes.Buffer with a mutex so the subprocess
// writer and assertion-side readers don't race.
type threadSafeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *threadSafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns a snapshot of all bytes written so far.
func (b *threadSafeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// freePort grabs a kernel-assigned port from the wildcard.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("freePort close: %v", err)
	}
	return port
}

// startCM launches CM with the given config, polls /healthz.
func startCM(t *testing.T, configPath string, port int) *process {
	t.Helper()
	return startProcess(t, "cm", cmBinary, configPath,
		fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
}

// startRunner launches the runner with the given config, polls /readyz.
func startRunner(t *testing.T, configPath string, port int) *process {
	t.Helper()
	return startProcess(t, "runner", runnerBinary, configPath,
		fmt.Sprintf("http://127.0.0.1:%d/readyz", port))
}

func startProcess(t *testing.T, name, binary, configPath, healthURL string) *process {
	t.Helper()

	stderr := &threadSafeBuffer{}
	cmd := exec.Command(binary, "--config", configPath)
	cmd.Stderr = stderr
	cmd.Stdout = stderr // pool both for grep simplicity

	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}

	p := &process{cmd: cmd, stderr: stderr}

	t.Cleanup(func() {
		if cmd.Process == nil {
			return
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})

	if err := waitForReady(healthURL, 30*time.Second); err != nil {
		t.Fatalf("%s did not become ready: %v\nstderr:\n%s", name, err, stderr.String())
	}

	return p
}

func waitForReady(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 1 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			code := resp.StatusCode
			_ = resp.Body.Close()
			if code == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", url)
}

// hasLine returns true if the captured stderr contains needle.
func (p *process) hasLine(needle string) bool {
	return strings.Contains(p.stderr.String(), needle)
}
