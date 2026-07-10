//go:build integration

package integration_test

import (
	"bytes"
	"fmt"
	"io"
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

// freePort grabs a kernel-assigned loopback port.
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

// startCM launches CM (`contextmatrix --config <path>`), polls /healthz, and
// tees stdout+stderr into rl.cmSink (saved as cm.log). CM output is NOT
// forwarded to combined.log — it is high-volume request noise.
func startCM(t *testing.T, configPath string, port int, rl *runLog) *process {
	t.Helper()

	return startProcess(t, "cm", cmBinary, []string{"--config", configPath},
		fmt.Sprintf("http://127.0.0.1:%d/healthz", port), rl, rl.cmSink, false)
}

// startAgent launches the agent backend (`contextmatrix-agent serve --config
// <path>`), polls /readyz, and tees output into rl.agentSink (agent.log) AND
// forwards each line to combined.log so backend state changes interleave with
// the transcript.
func startAgent(t *testing.T, hostBinary, configPath string, port int, rl *runLog) *process {
	t.Helper()

	return startProcess(t, "agent", hostBinary, []string{"serve", "--config", configPath},
		fmt.Sprintf("http://127.0.0.1:%d/readyz", port), rl, rl.agentSink, true)
}

// startChatBackend launches the chat backend (`contextmatrix-chat serve
// --config <path>`), polls /readyz, and tees output into rl.chatSink (chat.log)
// plus combined.log.
func startChatBackend(t *testing.T, hostBinary, configPath string, port int, rl *runLog) *process {
	t.Helper()

	return startProcess(t, "chat", hostBinary, []string{"serve", "--config", configPath},
		fmt.Sprintf("http://127.0.0.1:%d/readyz", port), rl, rl.chatSink, true)
}

func startProcess(t *testing.T, name, binary string, args []string, healthURL string, rl *runLog, sink *bytes.Buffer, forwardToCombined bool) *process {
	t.Helper()

	stderr := &threadSafeBuffer{}

	// Tee subprocess output to: (1) the thread-safe buffer for in-test grep,
	// (2) the per-source sink saved as <name>.log, and (3) optionally the
	// chronological combined log.
	multiWriter := io.MultiWriter(stderr, sink)

	onLine := func(string) {}
	if forwardToCombined {
		onLine = func(line string) { rl.writeLine(name, line) }
	}

	tee := newLineTee(multiWriter, onLine)

	cmd := exec.Command(binary, args...)
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

		// Emit any partial trailing line buffered by the tee at exit.
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

// tail returns the last n lines of s, joined with newlines. Used for
// truncated error context.
func tail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}

	return strings.Join(lines[len(lines)-n:], "\n")
}
