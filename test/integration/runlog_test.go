//go:build integration

package integration_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
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
func (r *runLog) finalize(scenarioID, status string, duration time.Duration, transcriptJSONL []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_ = os.WriteFile(filepath.Join(r.dir, "cm.log"), r.cmSink.Bytes(), 0o644)
	_ = os.WriteFile(filepath.Join(r.dir, "runner.log"), r.runnerSink.Bytes(), 0o644)

	if len(transcriptJSONL) > 0 {
		_ = os.WriteFile(filepath.Join(r.dir, "transcript.jsonl"), transcriptJSONL, 0o644)
	}

	combinedPath := filepath.Join(r.dir, "combined.log")
	combinedBytes, _ := os.ReadFile(combinedPath)

	report := buildMarkdownReport(scenarioID, status, duration,
		combinedBytes, r.cmSink.Bytes(), r.runnerSink.Bytes(), transcriptJSONL)
	_ = os.WriteFile(filepath.Join(r.dir, "run.md"), report, 0o644)

	if r.combined != nil {
		_ = r.combined.Close()
		r.combined = nil
	}
}

// buildMarkdownReport assembles a per-FSM-phase markdown view of the
// run. Each section corresponds to one FSM state the orchestrator
// entered (Planning, CreatingSubtasks, Executing, …) and contains the
// combined log lines that occurred while the FSM was in that state.
// CM logs are intentionally excluded — they are high-volume HTTP
// request noise that drowns out the chat-loop story.
func buildMarkdownReport(scenarioID, status string, duration time.Duration,
	combined, _, _, _ []byte,
) []byte {
	var b bytes.Buffer

	fmt.Fprintf(&b, "# %s\n\n", scenarioID)
	fmt.Fprintf(&b, "- **Status**: %s\n", status)
	fmt.Fprintf(&b, "- **Duration**: %s\n", duration.Truncate(100*time.Millisecond))
	fmt.Fprintf(&b, "- **Captured**: %s\n\n", time.Now().UTC().Format(time.RFC3339))

	phases := splitByPhase(combined)

	if len(phases) == 0 {
		fmt.Fprintf(&b, "_No FSM phase transitions detected — the run did not reach the orchestrator._\n\n")
		writeFenced(&b, "log", combined)

		return b.Bytes()
	}

	fmt.Fprintf(&b, "Each section below is one FSM state the orchestrator entered, in order. ")
	fmt.Fprintf(&b, "Lines tagged `[runner]` come from the runner subprocess; `[user_chat]` is ")
	fmt.Fprintf(&b, "the simulated human's chat message; `[transcript:<kind>]` is the agent's ")
	fmt.Fprintf(&b, "stream-json output (`thinking`, `text`, `tool_call`, etc.) as seen by ")
	fmt.Fprintf(&b, "CM's `/api/runner/logs` SSE consumer.\n\n")

	for _, p := range phases {
		fmt.Fprintf(&b, "## %s\n\n", p.title)
		writeFenced(&b, "log", p.body)
	}

	return b.Bytes()
}

// phaseSection is one FSM-state slice of the combined log.
type phaseSection struct {
	title string // human-readable name, e.g. "Planning"
	body  []byte // the lines from combined.log that fall in this state
}

// phaseEnterRE matches the runner's "executing action=X state=Y" log
// line that fires when the FSM enters a new state. Y is the state name.
// The regex tolerates the slog-style key=value formatting (state may be
// quoted or bare; action is logged as a bare identifier today).
var phaseEnterRE = regexp.MustCompile(`msg=executing action=\w+ state="?(\w+)"?`)

// splitByPhase walks the combined log line-by-line, starting a new
// section every time the runner emits a state-entry log. Lines that
// arrive before the first state entry are grouped under "Setup".
// Consecutive entries to the same state are folded into a single
// section (an FSM may re-enter a state via revision loops; in that
// case we deliberately keep them merged for readability — the operator
// can grep the raw runner.log for finer detail).
func splitByPhase(combined []byte) []phaseSection {
	if len(combined) == 0 {
		return nil
	}

	var (
		sections   []phaseSection
		current    phaseSection
		inFirst    bool
		lastState  string
		bodyBuf    bytes.Buffer
		flush      = func() {
			if !inFirst && bodyBuf.Len() == 0 {
				return
			}
			current.body = append([]byte(nil), bodyBuf.Bytes()...)
			sections = append(sections, current)
			bodyBuf.Reset()
		}
	)

	current = phaseSection{title: "Setup"}
	inFirst = true

	for _, line := range bytes.Split(combined, []byte("\n")) {
		if len(line) == 0 {
			continue
		}

		if m := phaseEnterRE.FindSubmatch(line); m != nil {
			state := string(m[1])
			if state != lastState {
				flush()
				current = phaseSection{title: humanizeState(state)}
				lastState = state
				inFirst = false
			}
		}

		bodyBuf.Write(line)
		bodyBuf.WriteByte('\n')
	}

	flush()

	return sections
}

// humanizeState turns a CamelCase FSM state name into "Camel Case"
// for readable section headings. Acronyms aren't preserved — the FSM
// states don't currently use any.
func humanizeState(s string) string {
	var b strings.Builder

	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) {
			b.WriteByte(' ')
		}

		b.WriteRune(r)
	}

	return b.String()
}

// writeFenced writes a fenced code block, falling back to a placeholder
// when the content is empty.
func writeFenced(b *bytes.Buffer, lang string, content []byte) {
	fmt.Fprintf(b, "```%s\n", lang)

	if len(content) == 0 {
		b.WriteString("(empty)\n")
	} else {
		b.Write(content)

		if content[len(content)-1] != '\n' {
			b.WriteByte('\n')
		}
	}

	b.WriteString("```\n\n")
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
