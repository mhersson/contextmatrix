package main

import (
	"encoding/json"
	"fmt"
)

// streamJSONEvent is the wire shape of a single stream-json event from
// Claude Code. The runner's logparser unmarshals these.
type streamJSONEvent struct {
	Type    string  `json:"type"`
	Message message `json:"message,omitempty"`
}

type message struct {
	Role    string         `json:"role,omitempty"`
	Content []contentBlock `json:"content,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// emitText writes a single assistant text event to stdout.
func emitText(text string) {
	ev := streamJSONEvent{
		Type: "assistant",
		Message: message{
			Role:    "assistant",
			Content: []contentBlock{{Type: "text", Text: text}},
		},
	}
	b, err := json.Marshal(ev)
	if err != nil {
		// Genuinely shouldn't happen with our typed input.
		panic(fmt.Errorf("emitText marshal: %w", err))
	}
	fmt.Println(string(b))
}

// emitMarker emits the canned stream-json sequence for a phase: one
// liveliness text event, then a final text event whose body contains
// the marker header followed by an optional fenced JSON payload.
func emitMarker(p phase, cardID string) {
	_ = cardID // Reserved for future per-phase customisation.

	emitText(fmt.Sprintf("stub claude faking phase: %s", p))

	switch p {
	case phasePlan:
		// PLAN_DRAFTED with a single canary subtask.
		payload := map[string]any{
			"plan_summary": "stub canary plan: one subtask that emits TEST-MARKER",
			"chosen_repos": []string{},
			"subtasks": []map[string]any{
				{
					"title":       "stub-canary-subtask",
					"description": "Append a TEST-MARKER comment to README.md and commit. Stub: this subtask is materialised by the FSM but its execute phase is also faked.",
					"repos":       []string{},
					"priority":    "medium",
					"depends_on":  []string{},
				},
			},
		}
		emitText(formatMarker("PLAN_DRAFTED", payload))

	case phaseSubtaskExec:
		// TASK_COMPLETE with a one-line summary. No JSON payload required.
		emitText(formatMarkerNoPayload("TASK_COMPLETE", "stub: canary marker appended"))

	case phaseDocs:
		emitText(formatMarkerNoPayload("DOCS_WRITTEN", "stub: docs section written"))

	case phaseReview:
		// REVIEW_FINDINGS with approve recommendation.
		payload := map[string]any{
			"recommendation": "approve",
			"summary":        "stub: trivial canary, no concerns",
			"findings":       []any{},
		}
		emitText(formatMarker("REVIEW_FINDINGS", payload))

	case phaseDiagnose:
		payload := map[string]any{
			"summary":        "stub: no investigation needed",
			"recommendation": "proceed",
		}
		emitText(formatMarker("DIAGNOSIS_COMPLETE", payload))

	case phaseBrainstorm:
		// Brainstorming has its own multi-turn loop; for the v1 autonomous
		// scenario we don't exercise it. If we hit it, emit a no-op
		// completion so the FSM doesn't hang.
		emitText("stub: brainstorming not exercised in v1 autonomous scenario")

	default:
		// Unknown phase already exited fatally in main; this is unreachable.
		panic(fmt.Errorf("emitMarker called with unknown phase"))
	}
}

// formatMarker renders a marker header followed by a fenced JSON block.
// Matches the regex in claudeclient.fencedJSONRe.
func formatMarker(name string, payload any) string {
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		panic(fmt.Errorf("formatMarker marshal: %w", err))
	}
	return fmt.Sprintf("%s\n\n```json\n%s\n```", name, string(b))
}

// formatMarkerNoPayload renders a marker header with a one-line summary
// (no fenced JSON). Used by markers like TASK_COMPLETE where the runner
// doesn't require a JSON payload.
func formatMarkerNoPayload(name, summary string) string {
	return fmt.Sprintf("%s\n\n%s", name, summary)
}
