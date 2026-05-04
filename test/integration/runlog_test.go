//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// runLog is the per-scenario observability writer. It owns:
//
//   - dir: a fresh /tmp/cm-int-runs/<scenarioID>-<timestamp>/ directory.
//   - combined: a chronological merge of every line emitted by the
//     subprocesses (cm, runner) plus transcript SSE events plus
//     test-side chat sends, each prefixed with [<source>] and an
//     RFC3339Nano timestamp. Useful for "what happened, in order?"
//     post-mortems.
//
// Subprocess outputs are also written to per-source files (cm.log,
// runner.log) so callers that want to grep one stream can do so without
// mining the combined log. The transcript SSE stream is captured to
// transcript.jsonl by the existing analyzer_test code.
type runLog struct {
	mu       sync.Mutex
	dir      string
	combined *os.File

	cmSink     *bytes.Buffer
	runnerSink *bytes.Buffer
}

// runlogCard mirrors the subset of CM's card JSON shape that
// buildMarkdownReport needs. Defined locally so the runlog package stays
// independent of internal/board imports.
type runlogCard struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	State         string `json:"state"`
	Parent        string `json:"parent,omitempty"`
	AssignedAgent string `json:"assigned_agent"`
	Body          string `json:"body"`
	TokenUsage    *struct {
		PromptTokens     int64   `json:"prompt_tokens"`
		CompletionTokens int64   `json:"completion_tokens"`
		EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	} `json:"token_usage,omitempty"`
	ActivityLog []struct {
		Agent     string `json:"agent"`
		Timestamp string `json:"ts"`
		Action    string `json:"action"`
		Message   string `json:"message"`
		Skill     string `json:"skill,omitempty"`
	} `json:"activity_log,omitempty"`
}

// newRunLog creates the per-scenario output directory and opens
// combined.log for live chronological writes. The directory persists
// past test cleanup so the operator can inspect it after the run.
func newRunLog(scenarioID string) (*runLog, error) {
	dir := filepath.Join(os.TempDir(), "cm-int-runs",
		fmt.Sprintf("%s-%s", scenarioID, time.Now().Format("20060102T150405.000")))

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("runlog mkdir %s: %w", dir, err)
	}

	combinedPath := filepath.Join(dir, "combined.log")

	f, err := os.Create(combinedPath)
	if err != nil {
		return nil, fmt.Errorf("runlog create %s: %w", combinedPath, err)
	}

	return &runLog{
		dir:        dir,
		combined:   f,
		cmSink:     &bytes.Buffer{},
		runnerSink: &bytes.Buffer{},
	}, nil
}

// writeLine appends a single timestamped line tagged with source to the
// combined log. Empty lines are suppressed.
func (r *runLog) writeLine(source, line string) {
	if line == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	fmt.Fprintf(r.combined, "[%s] [%s] %s\n",
		time.Now().UTC().Format(time.RFC3339Nano), source, line)
}

// finalize flushes per-source files (cm.log, runner.log) and the
// markdown report, then closes the combined log handle. Safe to call
// multiple times.
//
// worker.raw.jsonl is written live by startWorkerCapture; finalize reads
// it from disk here so the markdown summary reflects whatever was captured.
func (r *runLog) finalize(scenarioID, status string, duration time.Duration, transcriptJSONL []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_ = os.WriteFile(filepath.Join(r.dir, "cm.log"), r.cmSink.Bytes(), 0o644)
	_ = os.WriteFile(filepath.Join(r.dir, "runner.log"), r.runnerSink.Bytes(), 0o644)

	if len(transcriptJSONL) > 0 {
		_ = os.WriteFile(filepath.Join(r.dir, "transcript.jsonl"), transcriptJSONL, 0o644)
	}

	// Read worker.raw.jsonl written by the live-capture goroutine.
	workerRawJSONL, _ := os.ReadFile(filepath.Join(r.dir, "worker.raw.jsonl"))

	combinedPath := filepath.Join(r.dir, "combined.log")
	combinedBytes, _ := os.ReadFile(combinedPath)

	// cards.json is written by the per-scenario cleanup that runs before
	// CM shuts down. Empty when the snapshot HTTP call failed.
	cardsJSON, _ := os.ReadFile(filepath.Join(r.dir, "cards.json"))

	report := buildMarkdownReport(scenarioID, status, duration,
		combinedBytes, transcriptJSONL, workerRawJSONL, cardsJSON)
	_ = os.WriteFile(filepath.Join(r.dir, "run.md"), report, 0o644)

	if r.combined != nil {
		_ = r.combined.Close()
		r.combined = nil
	}
}

func buildMarkdownReport(scenarioID, status string, duration time.Duration, combined, transcriptJSONL, workerRawJSONL, cardsJSON []byte) []byte {
	var b bytes.Buffer

	fmt.Fprintf(&b, "# %s — %s — %s\n\n", scenarioID, status, duration.Round(time.Millisecond))

	fmt.Fprintln(&b, "## Timeline")
	fmt.Fprintln(&b)

	if combined := bytes.TrimSpace(combined); len(combined) > 0 {
		b.WriteString("```\n")
		b.Write(combined)
		b.WriteString("\n```\n\n")
	} else {
		fmt.Fprintln(&b, "_combined.log empty_")
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b, "## Worker raw stream-json summary")
	fmt.Fprintln(&b)

	if len(workerRawJSONL) == 0 {
		fmt.Fprintln(&b, "_no worker stdout captured (container already gone or stub image absent)_")
		fmt.Fprintln(&b)
	} else {
		types, tools := summarizeWorkerStream(workerRawJSONL)

		fmt.Fprintln(&b, "Events by type:")
		fmt.Fprintln(&b)

		for _, k := range sortedKeys(types) {
			fmt.Fprintf(&b, "- %s: %d\n", k, types[k])
		}

		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "MCP tool calls:")
		fmt.Fprintln(&b)

		for _, k := range sortedKeys(tools) {
			fmt.Fprintf(&b, "- %s: %d\n", k, tools[k])
		}

		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "Full file: `worker.raw.jsonl`")
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b, "## Transcript summary")
	fmt.Fprintln(&b)

	if len(transcriptJSONL) == 0 {
		fmt.Fprintln(&b, "_transcript.jsonl empty (no SSE events captured)_")
		fmt.Fprintln(&b)
	} else {
		types := summarizeTranscript(transcriptJSONL)

		fmt.Fprintln(&b, "Events by type:")
		fmt.Fprintln(&b)

		for _, k := range sortedKeys(types) {
			fmt.Fprintf(&b, "- %s: %d\n", k, types[k])
		}

		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "Full file: `transcript.jsonl`")
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b, "## Card final state")
	fmt.Fprintln(&b)
	renderCardsSection(&b, cardsJSON)

	fmt.Fprintln(&b, "## Activity log (chronological)")
	fmt.Fprintln(&b)
	renderActivityLogSection(&b, cardsJSON)

	fmt.Fprintln(&b, "## CM and runner stderr")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "- `cm.log` — CM stderr verbatim")
	fmt.Fprintln(&b, "- `runner.log` — runner stderr verbatim")

	return b.Bytes()
}

// parseRunlogCards decodes cardsJSON, accepting either CM's
// {"items":[...]} envelope (what the snapshot cleanup writes) or a raw
// [...] array (what unit tests pass for convenience). Returns nil and
// the parse error when neither shape applies.
func parseRunlogCards(cardsJSON []byte) ([]runlogCard, error) {
	if len(cardsJSON) == 0 {
		return nil, nil
	}

	// Envelope first — that's what cards.json contains in production.
	var env struct {
		Items []runlogCard `json:"items"`
	}
	if err := json.Unmarshal(cardsJSON, &env); err == nil && env.Items != nil {
		return env.Items, nil
	}

	// Raw array fallback (unit tests).
	var arr []runlogCard
	if err := json.Unmarshal(cardsJSON, &arr); err != nil {
		return nil, err
	}

	return arr, nil
}

// renderCardsSection writes one section per card with state, agent,
// token usage, and parent reference. Renders nothing meaningful when
// cardsJSON is empty (snapshot wasn't captured) — callers always
// include a pointer to cards.json elsewhere in the report.
func renderCardsSection(b *bytes.Buffer, cardsJSON []byte) {
	if len(cardsJSON) == 0 {
		fmt.Fprintln(b, "_cards.json not captured_")
		fmt.Fprintln(b)

		return
	}

	cards, err := parseRunlogCards(cardsJSON)
	if err != nil {
		fmt.Fprintf(b, "_cards.json parse error: %v_\n\n", err)

		return
	}

	if len(cards) == 0 {
		fmt.Fprintln(b, "_no cards in project_")
		fmt.Fprintln(b)

		return
	}

	// Sort: parents before subtasks, alphabetical within each group.
	sort.SliceStable(cards, func(i, j int) bool {
		if (cards[i].Parent == "") != (cards[j].Parent == "") {
			return cards[i].Parent == ""
		}

		return cards[i].ID < cards[j].ID
	})

	for _, c := range cards {
		fmt.Fprintf(b, "### %s — %s\n\n", c.ID, c.Title)
		fmt.Fprintf(b, "- state: `%s`\n", c.State)
		fmt.Fprintf(b, "- agent: `%s`\n", c.AssignedAgent)

		if c.Parent != "" {
			fmt.Fprintf(b, "- parent: `%s`\n", c.Parent)
		}

		if c.TokenUsage != nil {
			fmt.Fprintf(b, "- token_usage: prompt=%d completion=%d cost=$%.4f\n",
				c.TokenUsage.PromptTokens, c.TokenUsage.CompletionTokens, c.TokenUsage.EstimatedCostUSD)
		} else {
			fmt.Fprintln(b, "- token_usage: _none_")
		}

		fmt.Fprintln(b)
	}
}

// renderActivityLogSection writes a chronological merge of all
// activity-log entries across all cards. Each line is prefixed with
// the card ID so the operator can scan one timeline.
func renderActivityLogSection(b *bytes.Buffer, cardsJSON []byte) {
	if len(cardsJSON) == 0 {
		fmt.Fprintln(b, "_cards.json not captured_")
		fmt.Fprintln(b)

		return
	}

	cards, err := parseRunlogCards(cardsJSON)
	if err != nil {
		fmt.Fprintf(b, "_cards.json parse error: %v_\n\n", err)

		return
	}

	type entry struct {
		ts     string
		card   string
		agent  string
		action string
		msg    string
		skill  string
	}

	var all []entry

	for _, c := range cards {
		for _, e := range c.ActivityLog {
			all = append(all, entry{
				ts: e.Timestamp, card: c.ID, agent: e.Agent,
				action: e.Action, msg: e.Message, skill: e.Skill,
			})
		}
	}

	sort.SliceStable(all, func(i, j int) bool { return all[i].ts < all[j].ts })

	if len(all) == 0 {
		fmt.Fprintln(b, "_no activity log entries_")
		fmt.Fprintln(b)

		return
	}

	b.WriteString("```\n")

	for _, e := range all {
		skill := ""
		if e.skill != "" {
			skill = " [skill=" + e.skill + "]"
		}

		fmt.Fprintf(b, "%s [%s] (%s) %s: %s%s\n",
			e.ts, e.card, e.agent, e.action, e.msg, skill)
	}

	b.WriteString("```\n\n")
}

// summarizeWorkerStream walks one-event-per-line stream-json and returns
// counts of {top-level event type} and {MCP tool name when present in
// tool_use frames}. Bad lines are skipped silently — the raw file is
// still on disk for forensic reading.
func summarizeWorkerStream(raw []byte) (map[string]int, map[string]int) {
	types := map[string]int{}
	tools := map[string]int{}

	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		// Real Claude stream-json wraps content blocks inside an
		// envelope: top-level frames are {type:"assistant"|"user", ...,
		// message:{content:[{type:"text"|"tool_use"|"tool_result", ...}]}}.
		// Top-level type counts the envelope; tool counts come from the
		// nested content blocks.
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Name string `json:"name"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}

		if ev.Type != "" {
			types[ev.Type]++
		}

		for _, block := range ev.Message.Content {
			if block.Type == "tool_use" && block.Name != "" {
				tools[block.Name]++
			}
		}
	}

	return types, tools
}

// summarizeTranscript walks the runner's parsed SSE-event JSONL and
// returns counts by event type. Same skip-bad-line behaviour.
func summarizeTranscript(raw []byte) map[string]int {
	types := map[string]int{}

	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var ev struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}

		if ev.Type != "" {
			types[ev.Type]++
		}
	}

	return types
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	sort.Strings(out)

	return out
}

// lineTee wraps an inner writer (e.g. a bytes.Buffer) so every byte is
// also forwarded line-by-line to a callback. Partial trailing lines are
// retained until a subsequent Write completes them; consumers that need
// to flush before EOF can call Flush.
type lineTee struct {
	mu      sync.Mutex
	inner   io.Writer
	onLine  func(string)
	pending []byte
}

func newLineTee(inner io.Writer, onLine func(string)) *lineTee {
	return &lineTee{inner: inner, onLine: onLine}
}

func (lt *lineTee) Write(p []byte) (int, error) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	n, err := lt.inner.Write(p)
	lt.pending = append(lt.pending, p[:n]...)

	for {
		idx := bytesIndexNewline(lt.pending)
		if idx < 0 {
			break
		}

		line := string(lt.pending[:idx])
		lt.pending = lt.pending[idx+1:]
		lt.onLine(line)
	}

	return n, err
}

func (lt *lineTee) Flush() {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	if len(lt.pending) == 0 {
		return
	}

	lt.onLine(string(lt.pending))
	lt.pending = nil
}

func bytesIndexNewline(p []byte) int {
	for i, c := range p {
		if c == '\n' {
			return i
		}
	}

	return -1
}
