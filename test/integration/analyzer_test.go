//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

// analyzeTranscript posts the captured transcript to Haiku for friction
// scoring. Returns a structured report or an error. Skipped (no error,
// nil report) if no API key is configured.
func analyzeTranscript(ctx context.Context, t *testing.T, events []transcriptEvent) (*frictionReport, error) {
	t.Helper()

	apiKey := analyzerAPIKey()
	if apiKey == "" {
		t.Logf("friction analyzer skipped: no ANTHROPIC_API_KEY (or analyzer-specific) set")

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

// printFrictionReport renders the report into the harness summary.
func printFrictionReport(scenarioName string, report *frictionReport) {
	if report == nil {
		fmt.Fprintf(os.Stderr, "Friction report (%s): skipped (no analyzer API key)\n", scenarioName)

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
