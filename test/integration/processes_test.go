//go:build integration

package integration_test

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
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

// startCM launches CM with the given config, polls /healthz, and tees
// stdout+stderr into rl.cmSink (saved as cm.log on disk). CM output is
// NOT forwarded to combined.log / run.md — it is high-volume request
// noise that drowns out the runner events the operator
// actually wants to read.
func startCM(t *testing.T, configPath string, port int, rl *runLog) *process {
	t.Helper()

	return startProcess(t, "cm", cmBinary, configPath,
		fmt.Sprintf("http://127.0.0.1:%d/healthz", port), rl, rl.cmSink, false)
}

// startRunner launches the runner with the given config, polls /readyz,
// and tees stdout+stderr into rl.runnerSink (saved as runner.log) AND
// forwards each line to combined.log so the runner state changes and
// chat-loop debug logs interleave with transcript / user_chat events.
// Runner state log lines (e.g., "Initializing", "Completing")
// surface at debug level — config_test.go writes log_level: debug.
func startRunner(t *testing.T, configPath string, port int, rl *runLog) *process {
	t.Helper()

	return startProcess(t, "runner", runnerBinary, configPath,
		fmt.Sprintf("http://127.0.0.1:%d/readyz", port), rl, rl.runnerSink, true)
}

func startProcess(t *testing.T, name, binary, configPath, healthURL string, rl *runLog, sink *bytes.Buffer, forwardToCombined bool) *process {
	t.Helper()

	stderr := &threadSafeBuffer{}

	// Tee subprocess output to: (1) the existing thread-safe buffer for
	// in-test grep, (2) the per-scenario raw sink saved on disk as
	// <name>.log, and (3) optionally the chronological combined log
	// (runner only — see startCM rationale).
	multiWriter := io.MultiWriter(stderr, sink)

	onLine := func(string) {}
	if forwardToCombined {
		onLine = func(line string) { rl.writeLine(name, line) }
	}

	tee := newLineTee(multiWriter, onLine)

	cmd := exec.Command(binary, "--config", configPath)
	cmd.Stderr = tee
	cmd.Stdout = tee

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

		// Emit any partial trailing line that was buffered by the tee
		// when the process exited mid-write so it appears in combined.log.
		tee.Flush()
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
