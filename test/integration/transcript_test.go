//go:build integration

package integration_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// transcriptEvent is the wire shape of one SSE event from CM's
// /api/runner/logs?card_id=X endpoint (see remote-execution.md §
// "ContextMatrix: GET /api/runner/logs"). The harness keeps the full
// JSON payload as RawJSON so the friction analyzer can see whatever
// fields CM's session log manager exposes.
type transcriptEvent struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	Time    string `json:"ts"`
	RawJSON string `json:"-"` // populated by the consumer
}

// transcriptBuffer is a per-scenario ring buffer of SSE events with a
// rough byte cap to avoid OOM on a runaway run.
type transcriptBuffer struct {
	mu      sync.Mutex
	events  []transcriptEvent
	bytes   int
	maxByte int
}

func newTranscriptBuffer(maxByte int) *transcriptBuffer {
	return &transcriptBuffer{maxByte: maxByte}
}

func (b *transcriptBuffer) append(ev transcriptEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	size := len(ev.RawJSON) + 64
	for b.bytes+size > b.maxByte && len(b.events) > 0 {
		b.bytes -= len(b.events[0].RawJSON) + 64
		b.events = b.events[1:]
	}
	b.events = append(b.events, ev)
	b.bytes += size
}

func (b *transcriptBuffer) snapshot() []transcriptEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]transcriptEvent, len(b.events))
	copy(out, b.events)
	return out
}

// startTranscriptCapture opens an SSE connection to CM's /api/runner/logs
// and pumps events into buf until ctx is cancelled or the stream ends.
// Returns immediately; goroutine runs in background. The harness cancels
// ctx in t.Cleanup so the goroutine exits.
//
// When rl is non-nil, each parsed event is also forwarded to the
// scenario's combined log with source "transcript:<type>" so a reader
// of combined.log sees the chat-loop events interleaved with subprocess
// stderr in chronological order.
func startTranscriptCapture(t *testing.T, cmBaseURL, project, cardID string, buf *transcriptBuffer, rl *runLog) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		url := fmt.Sprintf("%s/api/runner/logs?project=%s&card_id=%s", cmBaseURL, project, cardID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			t.Logf("transcript: build request: %v", err)
			return
		}
		req.Header.Set("X-Agent-ID", "human:harness")
		req.Header.Set("Accept", "text/event-stream")

		client := &http.Client{Timeout: 0} // no read timeout for SSE
		resp, err := client.Do(req)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				t.Logf("transcript: GET /api/runner/logs: %v", err)
			}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Logf("transcript: SSE returned HTTP %d", resp.StatusCode)
			return
		}

		// Parse SSE: each event is `data: <json>\n\n`. We accumulate
		// data: lines until a blank line, then unmarshal.
		reader := bufio.NewReader(resp.Body)
		var dataBuf strings.Builder
		for {
			if ctx.Err() != nil {
				return
			}
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				if dataBuf.Len() > 0 {
					raw := dataBuf.String()
					dataBuf.Reset()
					var ev transcriptEvent
					if err := json.Unmarshal([]byte(raw), &ev); err != nil {
						continue
					}
					ev.RawJSON = raw
					buf.append(ev)

					if rl != nil {
						source := "transcript"
						if ev.Type != "" {
							source = "transcript:" + ev.Type
						}
						rl.writeLine(source, ev.Content)
					}

					if ev.Type == "terminal" {
						return
					}
				}
				continue
			}
			if strings.HasPrefix(line, "data: ") {
				if dataBuf.Len() > 0 {
					dataBuf.WriteString("\n")
				}
				dataBuf.WriteString(strings.TrimPrefix(line, "data: "))
			}
		}
	}()

	// Tiny poll to give the goroutine a chance to connect before the
	// scenario starts emitting events. Avoids losing the first few.
	time.Sleep(100 * time.Millisecond)
}
