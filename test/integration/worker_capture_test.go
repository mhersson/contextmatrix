//go:build integration

package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// workerCapture holds the cancel function and done channel for a live
// worker log capture goroutine started by startWorkerCapture.
type workerCapture struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// stop cancels the capture context and waits (up to 5s) for the goroutine
// to finish flushing and closing the output file. Call stop BEFORE reading
// worker.raw.jsonl so the file is fully written.
func (wc *workerCapture) stop() {
	wc.cancel()

	select {
	case <-wc.done:
	case <-time.After(5 * time.Second):
	}
}

// startWorkerCapture launches a goroutine that:
//  1. Polls dockerListByScenario every 75ms until a container appears (or
//     the context is cancelled or 90s elapses without a container).
//  2. Runs "docker logs -f <id>" with combined stdout+stderr piped directly
//     to <runlogDir>/worker.raw.jsonl on disk.
//  3. Exits once the docker process exits (container removed) or the
//     context is cancelled.
//
// The file is created immediately so that finalize always finds it, even
// when no container ever appeared (the file will be empty in that case).
//
// Call stop() on the returned workerCapture BEFORE reading worker.raw.jsonl
// (i.e. before finalize) to ensure the file is fully flushed and closed.
func startWorkerCapture(rl *runLog, scenarioID string) *workerCapture {
	outPath := filepath.Join(rl.dir, "worker.raw.jsonl")

	// Create the file immediately — finalize reads it at cleanup time and a
	// missing file would silently fall back to the "no worker stdout" sentinel.
	f, err := os.Create(outPath)
	if err != nil {
		rl.writeLine("harness", "worker_capture: create "+outPath+": "+err.Error())
		// Return a no-op capture so callers don't need nil checks.
		noop := &workerCapture{cancel: func() {}, done: make(chan struct{})}
		close(noop.done)

		return noop
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		defer f.Close()

		// Phase 1: poll for the container.
		deadline := time.Now().Add(90 * time.Second)

		var containerID string

		for {
			if ctx.Err() != nil {
				return
			}

			if time.Now().After(deadline) {
				rl.writeLine("harness", "worker_capture: no container within 90s ("+scenarioID+")")

				return
			}

			ids := dockerListByScenario(scenarioID)
			if len(ids) > 0 {
				containerID = ids[0]

				break
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(75 * time.Millisecond):
			}
		}

		// Phase 2: attach docker logs -f and stream to disk.
		//nolint:gosec // containerID comes from docker ps output
		cmd := exec.CommandContext(ctx, "docker", "logs", "-f", containerID)
		cmd.Stdout = f
		cmd.Stderr = f

		if err := cmd.Run(); err != nil && ctx.Err() == nil {
			// Container removal causes a non-nil error; log for diagnostics only.
			rl.writeLine("harness", "worker_capture: docker logs -f "+containerID+": "+err.Error())
		}

		// Sync to ensure any OS-buffered writes land before finalize reads the file.
		_ = f.Sync()

		rl.writeLine("harness", "worker_capture: closed log for "+containerID+" ("+scenarioID+")")
	}()

	rl.writeLine("harness", "worker_capture: started for "+scenarioID+" -> "+outPath)

	return &workerCapture{cancel: cancel, done: done}
}
