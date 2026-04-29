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
		// Pure-spec PLAN_DRAFTED with no subtasks. The stub mode runs
		// against a placeholder repo URL (https://example.invalid/...)
		// that exists only to satisfy the runner's webhook validator;
		// any orchestrator code path that actually tries to clone it
		// will fail. Emitting zero subtasks keeps Execute a no-op so
		// the FSM walks plan → subtasks → execute → document → review
		// → commit → done without ever invoking CloneRepo. Real-Claude
		// mode (test/integration/scenarios_test.go) uses a real local
		// HTTPS git server and does exercise the Execute clone path.
		payload := map[string]any{
			"plan_summary": "stub pure-spec canary: no execute work; verifies FSM phase progression",
			"chosen_repos": []string{},
			"subtasks":     []map[string]any{},
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

// --- HITL chat-loop helpers ---

// emitSystemInit writes the {"type":"system","subtype":"init",...} frame
// the runner uses to capture the session_id at the start of a session.
func emitSystemInit(sessionID string) {
	out, _ := json.Marshal(map[string]any{
		"type":       "system",
		"subtype":    "init",
		"session_id": sessionID,
		"model":      "stub-haiku",
	})
	fmt.Println(string(out))
}

// emitSystemEnd writes the end-of-turn frame with token usage so the
// runner's chat-loop unblocks and looks for either a marker tool_use or
// the next user input.
func emitSystemEnd() {
	out, _ := json.Marshal(map[string]any{
		"type":    "system",
		"subtype": "end",
		"usage": map[string]any{
			"input_tokens":  10,
			"output_tokens": 5,
		},
	})
	fmt.Println(string(out))
}

// emitAssistantText is a thin alias for emitText kept around so the HITL
// loop reads symmetrically with emitToolUse.
func emitAssistantText(text string) {
	emitText(text)
}

// emitToolUse writes a stream-json tool_use frame the runner intercepts
// as a HITL terminal marker (plan_complete / review_approve /
// review_revise / discovery_complete).
func emitToolUse(name string, input any) {
	out, _ := json.Marshal(map[string]any{
		"type":  "tool_use",
		"name":  name,
		"input": input,
	})
	fmt.Println(string(out))
}

// planCompletePayload builds the thin terminal signal the runner
// intercepts to end the plan chat-loop. Structured plan data lives
// in the card body — see stubCanonicalPlanBody.
func planCompletePayload(cardID string) map[string]any {
	return map[string]any{
		"card_id":      cardID,
		"plan_summary": "stub HITL: chat-driven plan approved",
	}
}

// stubCanonicalPlanBody mirrors the runtime contract: the agent writes
// `## Plan` markdown plus a fenced JSON block via update_card, and the
// runner reads from there. The stub's HITL plan-approval turn must
// emit an update_card call before the thin plan_complete signal.
//
// Pure-spec body (empty subtasks) keeps Execute as a no-op so the FSM
// walks plan → subtasks → execute → document → review → commit → done
// without ever invoking CloneRepo.
func stubCanonicalPlanBody() string {
	const prose = "## Plan\n\nstub HITL: chat-driven plan approved.\n\n### Subtasks\n\n_(none — pure-spec canary)_\n\n"

	payload, _ := json.MarshalIndent(map[string]any{
		"plan_summary": "stub HITL: chat-driven plan approved",
		"chosen_repos": []string{},
		"subtasks":     []map[string]any{},
	}, "", "  ")

	return prose + "```json\n" + string(payload) + "\n```\n"
}

// reviewApprovePayload builds the review_approve tool_use input.
func reviewApprovePayload(cardID string) map[string]any {
	return map[string]any{
		"card_id": cardID,
		"summary": "stub HITL: reviewer approved",
	}
}

// reviewRevisePayload builds the review_revise tool_use input. The
// detailed feedback string carries forward into the next replan round
// so the test can assert on the revision attempts counter.
func reviewRevisePayload(cardID, userText string) map[string]any {
	feedback := userText
	if feedback == "" {
		feedback = "stub HITL: reviewer requested changes"
	}

	return map[string]any{
		"card_id":  cardID,
		"summary":  "stub HITL: reviewer requested revisions",
		"feedback": feedback,
	}
}

// jsonUnmarshal is a thin wrapper that lets main.go avoid a direct
// "encoding/json" import (keeps imports symmetric across both files).
func jsonUnmarshal(data string, v any) error {
	return json.Unmarshal([]byte(data), v)
}
