//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// frictionFinding is one structured observation about agent friction.
type frictionFinding struct {
	Severity       string `json:"severity"`       // low | medium | high
	Category       string `json:"category"`       // unclear-prompt | hoop-jumping | repeated-tool-calls | wrong-tool-selection | instruction-confusion | agent-self-correction | other
	Excerpt        string `json:"excerpt"`        // <=200 char quote from transcript
	Recommendation string `json:"recommendation"` // suggested fix
}

type frictionReport struct {
	Findings []frictionFinding `json:"findings"`
	Summary  string            `json:"summary"`
}

// saveTranscript writes the captured events to a deterministic path on
// disk so the run-then-review workflow (let Claude Code analyse the
// transcript inline) doesn't depend on an Anthropic API key. Path
// includes the unix timestamp so successive runs don't clobber each
// other. Returns the absolute path written.
func saveTranscript(t *testing.T, events []transcriptEvent, scenario string) string {
	t.Helper()

	dir := filepath.Join(os.TempDir(), "cm-int-transcripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("saveTranscript mkdir %s: %v", dir, err)

		return ""
	}

	path := filepath.Join(dir, fmt.Sprintf("%s-%d.jsonl", scenario, time.Now().Unix()))

	f, err := os.Create(path)
	if err != nil {
		t.Logf("saveTranscript create %s: %v", path, err)

		return ""
	}
	defer f.Close()

	// One event per line (JSONL). Each event's RawJSON is already a JSON
	// object as produced by CM's session log SSE; we keep them verbatim
	// so the inline analyser sees the same payload the runner saw.
	for _, ev := range events {
		if ev.RawJSON == "" {
			continue
		}

		if _, err := fmt.Fprintln(f, ev.RawJSON); err != nil {
			t.Logf("saveTranscript write: %v", err)

			return path
		}
	}

	return path
}

// analyzeTranscript posts the captured transcript to Haiku for friction
// scoring. Returns a structured report or an error. Skipped (no error,
// nil report) when CM_FRICTION_ANALYZER is unset (default) — the
// transcript is on disk via saveTranscript and the controlling Claude
// Code session can read+analyse it directly without paying for an
// extra API call. Set CM_FRICTION_ANALYZER=1 for unattended runs that
// need a self-contained scoring pass.
func analyzeTranscript(ctx context.Context, t *testing.T, events []transcriptEvent) (*frictionReport, error) {
	t.Helper()

	if os.Getenv("CM_FRICTION_ANALYZER") != "1" {
		return nil, nil
	}

	apiKey := analyzerAPIKey()
	if apiKey == "" {
		t.Logf("CM_FRICTION_ANALYZER=1 but no ANTHROPIC_API_KEY (or analyzer-specific) set; skipping")

		return nil, nil
	}

	transcript := renderTranscript(events)
	if len(transcript) > 50_000 {
		transcript = transcript[:50_000] + "\n... (truncated)"
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	systemPrompt := `You are evaluating an autonomous coding agent's run for friction signals.
Score the transcript on instruction clarity, friction, and workarounds.

For each finding, output:
- severity: "low" | "medium" | "high"
- category: one of "unclear-prompt", "hoop-jumping", "repeated-tool-calls", "wrong-tool-selection", "instruction-confusion", "agent-self-correction", "other"
- excerpt: a quote from the transcript <=200 characters
- recommendation: a concrete suggestion for fixing the prompt or tool

Return ONLY valid JSON, no prose, matching:
{"findings": [...], "summary": "one sentence overall judgment"}

If the agent ran cleanly with no notable friction, return {"findings": [], "summary": "clean run"}.`

	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	msg, err := client.Messages.New(cctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5,
		MaxTokens: 2048,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(
				"Transcript follows. Analyse for agent friction:\n\n" + transcript,
			)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("anthropic call: %w", err)
	}

	if len(msg.Content) == 0 {
		return nil, fmt.Errorf("anthropic returned empty content")
	}

	text := msg.Content[0].Text
	// Strip ``` fences if Haiku wraps the JSON.
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}

	var report frictionReport
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		return nil, fmt.Errorf("decode report: %w (raw: %.500s)", err, text)
	}

	return &report, nil
}

// analyzerAPIKey resolves the API key for the friction analyzer. Order:
// 1. ANTHROPIC_API_KEY env var (most explicit)
// 2. The runner's anthropic_api_key from config.yaml (only if set).
func analyzerAPIKey() string {
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		return v
	}

	if creds, _ := credsFromRunnerConfig(); creds != nil && creds.APIKey != "" {
		return creds.APIKey
	}

	return ""
}

// renderTranscript flattens the captured events into a plain text block
// suitable for the analyzer to read.
func renderTranscript(events []transcriptEvent) string {
	var b strings.Builder
	for _, ev := range events {
		fmt.Fprintf(&b, "[%s] %s: %s\n", ev.Time, ev.Type, ev.Content)
	}

	return b.String()
}

// printFrictionReport renders the analyzer's findings into the harness
// summary. Always prints the saved transcript path so the controller
// (Claude Code session OR the human operator) can read it manually.
func printFrictionReport(scenarioName, transcriptPath string, report *frictionReport) {
	if transcriptPath != "" {
		fmt.Fprintf(os.Stderr, "Transcript saved (%s): %s\n", scenarioName, transcriptPath)
	}

	if report == nil {
		fmt.Fprintf(os.Stderr, "Inline analysis: skipped (set CM_FRICTION_ANALYZER=1 + ANTHROPIC_API_KEY for self-contained scoring; otherwise read the transcript above)\n")

		return
	}

	if len(report.Findings) == 0 {
		fmt.Fprintf(os.Stderr, "Friction report (%s): %s - no findings\n", scenarioName, report.Summary)

		return
	}

	fmt.Fprintf(os.Stderr, "\nFriction report (%s):\n", scenarioName)
	fmt.Fprintf(os.Stderr, "  Summary: %s\n\n", report.Summary)

	// Group by severity, high first.
	for _, sev := range []string{"high", "medium", "low"} {
		for _, f := range report.Findings {
			if f.Severity != sev {
				continue
			}

			fmt.Fprintf(os.Stderr, "  [%s] %s: %s\n", f.Severity, f.Category, f.Excerpt)
			fmt.Fprintf(os.Stderr, "    recommendation: %s\n\n", f.Recommendation)
		}
	}
}
