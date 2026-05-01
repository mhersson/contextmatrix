// Package main is the contextmatrix integration-harness fake-claude CLI.
//
// Each invocation handles ONE turn:
//
//   - Plan / review phases — read one stream-json user-message frame from
//     stdin and decide what to emit based on the message shape:
//
//   - autonomous primer ("Begin planning work for" /
//     "Review phase for parent card") -> emit canned text marker
//     (PLAN_DRAFTED / REVIEW_FINDINGS) + EOF, runner parses it via
//     runEphemeralPhase
//
//   - HITL kickoff primer ("Please plan card" / "Please review parent
//     card") -> emit a synthetic proposal text + system_end without a
//     marker tool_use; runChatLoop registers no terminal marker, emits
//     add_log("phase", "<phase>_awaiting"), waits on ChatInputCh
//
//   - subsequent HITL turn (trigger word) -> emit the matching marker
//     tool_use; runChatLoop terminates the chat
//
//   - Other phases (docs / subtask-execute / diagnose / brainstorm) — emit
//     the canned text marker for autonomous one-shot runs and exit.
//
// The runner spawns a fresh stub per turn (with --resume on HITL
// subsequent turns). The stub never stays alive across turns.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

// phase identifies which FSM phase this stub invocation is faking.
// Detected from the system prompt's marker-name reference.
type phase int

const (
	phaseUnknown phase = iota
	phasePlan
	phaseSubtaskExec // execute-task subagent run
	phaseDocs
	phaseReview
	phaseDiagnose
	phaseBrainstorm
)

func (p phase) String() string {
	switch p {
	case phasePlan:
		return "plan"
	case phaseSubtaskExec:
		return "subtask-execute"
	case phaseDocs:
		return "docs"
	case phaseReview:
		return "review"
	case phaseDiagnose:
		return "diagnose"
	case phaseBrainstorm:
		return "brainstorm"
	default:
		return "unknown"
	}
}

func main() {
	// Match the subset of Claude Code flags the runner sets. We accept
	// them all to avoid "unknown flag" errors but only use the system
	// prompt for phase detection.
	var (
		systemPrompt string
		model        string
		allowedTools string
		resume       string
		print        bool
		verbose      bool
		inputFormat  string
		outputFormat string
	)
	flag.StringVar(&systemPrompt, "append-system-prompt", "", "")
	flag.StringVar(&model, "model", "", "")
	flag.StringVar(&allowedTools, "allowed-tools", "", "")
	flag.StringVar(&resume, "resume", "", "")
	flag.BoolVar(&print, "print", false, "")
	flag.BoolVar(&verbose, "verbose", false, "")
	flag.StringVar(&inputFormat, "input-format", "", "")
	flag.StringVar(&outputFormat, "output-format", "", "")
	flag.Parse()

	// Discard flags we accept for compatibility but don't act on.
	_ = allowedTools
	_ = resume
	_ = print
	_ = verbose
	_ = inputFormat
	_ = outputFormat

	if systemPrompt == "" {
		log.Fatalf("stub-claude: missing --append-system-prompt")
	}

	p := detectPhase(systemPrompt)
	if p == phaseUnknown {
		log.Fatalf("stub-claude: could not detect phase from system prompt; first 200 chars: %.200s", systemPrompt)
	}

	cardID := os.Getenv("CM_CARD_ID")

	switch p {
	case phasePlan, phaseReview, phaseBrainstorm:
		// Plan, review, and brainstorm can run in autonomous (one-shot
		// text marker / immediate marker) or HITL (multi-turn chat).
		// Branch on the user-message shape.
		runPlanReviewTurn(p, cardID, model)
	default:
		log.Printf("stub-claude: detected phase=%s mode=autonomous model=%s", p, model)
		emitMarker(p, cardID)
	}
}

// runPlanReviewTurn reads exactly one user-message frame from stdin
// (the runner sends one frame per invocation and CloseStdins
// afterwards) and dispatches based on the message shape.
func runPlanReviewTurn(p phase, cardID, model string) {
	userText := readUserFrame(os.Stdin)

	log.Printf("stub-claude: phase=%s model=%s user=%q", p, model, truncateForLog(userText, 80))

	switch {
	case looksLikeAutonomousPrimer(p, userText):
		// Autonomous one-shot — emit canned text marker. The runner's
		// runEphemeralPhase parses the marker out of the text stream
		// and is happy with EOF as terminator.
		log.Printf("stub-claude: phase=%s mode=autonomous", p)
		emitMarker(p, cardID)
	case looksLikeHITLKickoff(p, userText):
		// HITL first turn — agent-first protocol: emit a synthetic
		// proposal text and end the turn without a marker tool_use.
		// The runner's runChatLoop sees no terminal marker, emits
		// add_log("phase", "<phase>_awaiting"), and waits for human input.
		log.Printf("stub-claude: phase=%s mode=hitl turn=first", p)
		emitSystemInit("stub-hitl-" + cardID)
		emitFirstTurnProposal(p)
		emitSystemEnd()
	default:
		// HITL subsequent turn — interpret the user's text.
		log.Printf("stub-claude: phase=%s mode=hitl turn=subsequent", p)
		emitSystemInit("stub-hitl-" + cardID)
		emitHITLSubsequent(p, cardID, userText)
	}
}

// emitHITLSubsequent emits the appropriate response for a HITL chat
// turn that arrives after the agent's first-turn proposal: a marker
// tool_use on a trigger word, or an "Acknowledged" continuation.
func emitHITLSubsequent(p phase, cardID, userText string) {
	decision := detectHITLDecision(p, userText)

	switch decision {
	case "plan_complete":
		emitAssistantText("Plan approved. Updating card body and calling plan_complete.")
		emitToolUse("update_card", map[string]any{
			"card_id": cardID,
			"body":    stubCanonicalPlanBody(),
		})
		emitToolUse("plan_complete", planCompletePayload(cardID))
	case "review_approve":
		emitAssistantText("Review approved. Calling review_approve.")
		emitToolUse("review_approve", reviewApprovePayload(cardID))
	case "review_revise":
		emitAssistantText("Review wants changes. Calling review_revise.")
		emitToolUse("review_revise", reviewRevisePayload(cardID, userText))
	case "discovery_complete":
		// Brainstorm phase: the prompt's "Promotion mid-dialogue"
		// handler instructs the agent to write the synthesized Design
		// to the card body via update_card BEFORE emitting
		// discovery_complete. Replicate that contract here so the
		// integration test can assert the body update happened.
		emitAssistantText("Promotion received. Capturing design and calling discovery_complete.")
		emitToolUse("update_card", map[string]any{
			"card_id": cardID,
			"body":    stubCanonicalDesignBody(),
		})
		emitToolUse("discovery_complete", discoveryCompletePayload(cardID))
	default:
		emitAssistantText("Acknowledged: " + truncateForLog(userText, 120))
	}

	emitSystemEnd()
}

// readUserFrame consumes one stream-json user-message frame from r and
// returns the text content. Empty if r closes before any frame arrives.
func readUserFrame(r io.Reader) string {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 8*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		return parseUserMessageText(line)
	}

	return ""
}

// looksLikeAutonomousPrimer recognises the kickoff verbs emitted by
// runEphemeralPhase (autonomous one-shot path). On match, the stub
// emits the canned text marker for the phase.
func looksLikeAutonomousPrimer(p phase, s string) bool {
	switch p {
	case phasePlan:
		// "Begin planning work for card `X`..." (with-card path) and
		// "Begin planning the work for this card." (c == nil fallback).
		return strings.Contains(s, "Begin planning work for") ||
			strings.Contains(s, "Begin planning the work for")
	case phaseReview:
		return strings.Contains(s, "Review phase for parent card")
	}

	return false
}

// looksLikeHITLKickoff recognises the kickoff verbs emitted by
// runChatLoop (HITL first turn). On match, the stub emits a synthetic
// proposal text without a marker tool_use.
func looksLikeHITLKickoff(p phase, s string) bool {
	switch p {
	case phasePlan:
		return strings.Contains(s, "Please plan card")
	case phaseReview:
		return strings.Contains(s, "Please review parent card")
	case phaseBrainstorm:
		// buildBrainstormPriming in the runner produces "Please
		// brainstorm card `X` with me." as the first user frame.
		return strings.Contains(s, "Please brainstorm card")
	}

	return false
}

// emitFirstTurnProposal emits a canned proposal text appropriate for
// the phase. The runner's runChatLoop does NOT match this as a
// terminal marker; it loops back and waits for the next chat input.
func emitFirstTurnProposal(p phase) {
	switch p {
	case phasePlan:
		emitAssistantText("Stub proposal: one subtask titled 'Implement main.go and main_test.go per the card body, commit, push'. Approve when ready.")
	case phaseReview:
		emitAssistantText("Stub review: diff implements the spec; recommend approve. Send back if you disagree.")
	case phaseBrainstorm:
		emitAssistantText("Stub brainstorm: which transport — REST or gRPC? Reply 'approve' to accept REST, or describe the design you want.")
	default:
		emitAssistantText(fmt.Sprintf("Stub: acknowledged kickoff for phase %s", p))
	}
}

// detectPhase scans the prompt for marker names. Each phase's prompt
// instructs the agent to emit the corresponding marker at end of phase,
// so the marker name is unambiguous in the prompt content.
func detectPhase(prompt string) phase {
	switch {
	case strings.Contains(prompt, "PLAN_DRAFTED") || strings.Contains(prompt, "plan_complete"):
		return phasePlan
	case strings.Contains(prompt, "DOCS_WRITTEN"):
		return phaseDocs
	case strings.Contains(prompt, "REVIEW_FINDINGS") || strings.Contains(prompt, "review_approve"):
		return phaseReview
	case strings.Contains(prompt, "DIAGNOSIS_COMPLETE"):
		return phaseDiagnose
	case strings.Contains(prompt, "TASK_COMPLETE") || strings.Contains(prompt, "TASK_NEEDS_DECOMPOSITION"):
		// execute-task prompt instructs subagent to emit TASK_COMPLETE
		// (or TASK_NEEDS_DECOMPOSITION when the work is too large).
		return phaseSubtaskExec
	case strings.Contains(prompt, "brainstorm") || strings.Contains(strings.ToLower(prompt), "brainstorming"):
		return phaseBrainstorm
	default:
		return phaseUnknown
	}
}

// detectHITLDecision parses the user message for trigger words that
// drive the terminal marker. Empty string means "no decision yet, keep
// chatting".
func detectHITLDecision(p phase, userText string) string {
	lower := strings.ToLower(userText)

	// Promotion canned message: the runner's driver pushes this into
	// ChatInputCh when /promote fires mid-run. The agent (real or stub)
	// terminates the active chat session by emitting the appropriate
	// terminal-marker tool. Match before the per-phase logic so the
	// canonical-text check wins over any stray "approve" substring.
	if strings.Contains(lower, "promoted this card to autonomous") {
		switch p {
		case phasePlan:
			return "plan_complete"
		case phaseReview:
			return "review_approve"
		case phaseBrainstorm:
			return "discovery_complete"
		}
	}

	switch p {
	case phasePlan:
		if strings.Contains(lower, "approve") {
			return "plan_complete"
		}
	case phaseReview:
		if strings.Contains(lower, "revise") {
			return "review_revise"
		}

		if strings.Contains(lower, "approve") {
			return "review_approve"
		}
	case phaseBrainstorm:
		if strings.Contains(lower, "approve") {
			return "discovery_complete"
		}
	}

	return ""
}

// parseUserMessageText extracts the first text block out of a stream-json
// user message frame: {"type":"user","message":{"content":[{"type":"text","text":"..."}]}}
// Best-effort: if parsing fails the line is treated as an opaque token.
func parseUserMessageText(line string) string {
	var frame struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}

	if err := jsonUnmarshal(line, &frame); err != nil {
		return line
	}

	for _, b := range frame.Message.Content {
		if b.Type == "text" {
			return b.Text
		}
	}

	return ""
}

func truncateForLog(s string, max int) string {
	if len(s) > max {
		return s[:max] + "…"
	}

	return s
}
