// Package main is the contextmatrix integration-harness fake-claude CLI.
//
// Operates in two modes:
//
//   - Autonomous / one-shot: detect phase from system prompt, emit canned
//     stream-json with the matching structured marker, exit. The runner
//     spawns a fresh process per phase.
//
//   - HITL chat-loop: detect via the "## HITL mode (chat-loop)" section
//     present in plan/replan/review prompts. Read stream-json user messages
//     from stdin one per line; for each user turn, emit assistant text and
//     (when the user approves / revises) emit a tool_use marker that drives
//     the runner's terminal-marker path. The process stays alive across
//     turns until stdin closes or the marker tool fires.
package main

import (
	"bufio"
	"flag"
	"fmt"
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

	// Discard flags we accept for compatibility but don't act on yet.
	_ = allowedTools
	_ = resume
	_ = print
	_ = verbose
	_ = outputFormat

	if systemPrompt == "" {
		log.Fatalf("stub-claude: missing --append-system-prompt")
	}

	p := detectPhase(systemPrompt)
	if p == phaseUnknown {
		log.Fatalf("stub-claude: could not detect phase from system prompt; first 200 chars: %.200s", systemPrompt)
	}

	cardID := os.Getenv("CM_CARD_ID")

	// HITL mode: prompt contains the chat-loop guidance section. Read
	// stream-json user messages from stdin until the user approves /
	// revises (which drives the marker tool_use) or stdin closes.
	if isHITLMode(systemPrompt, inputFormat) {
		log.Printf("stub-claude: detected phase=%s mode=hitl model=%s", p, model)
		runHITLChatLoop(p, cardID)

		return
	}

	log.Printf("stub-claude: detected phase=%s mode=autonomous model=%s", p, model)
	emitMarker(p, cardID)
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

// isHITLMode is true when the prompt was built by the runner's session
// path (Acquire's tier-3 spawn appends "Resuming from saved state:" + the
// primer to the system prompt). Autonomous one-shot phases construct the
// system prompt without that suffix; the chat-loop guidance section
// alone is not enough to discriminate because the prompt files contain
// it unconditionally for both modes.
func isHITLMode(prompt, inputFormat string) bool {
	_ = inputFormat // kept for future signal layering

	if !strings.Contains(prompt, "HITL mode (chat-loop)") {
		return false
	}

	return strings.Contains(prompt, "Resuming from saved state:")
}

// runHITLChatLoop reads stream-json user messages from stdin and emits
// assistant turns. The first user message is the primer (sent by the
// runner via Session.SendMessage); subsequent messages are the
// integration test's chat input. The loop terminates when the user
// message contains an "approve" / "revise" trigger word.
func runHITLChatLoop(p phase, cardID string) {
	emitSystemInit("stub-hitl-" + cardID)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 8*1024*1024)

	turn := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		userText := parseUserMessageText(line)
		turn++

		log.Printf("stub-claude: hitl turn=%d phase=%s user=%q", turn, p, truncateForLog(userText, 80))

		// Detect the test's terminal trigger. Each phase has its own
		// vocabulary; the HITL-stub scenarios drive the conversation
		// using these magic words.
		decision := detectHITLDecision(p, userText)

		switch decision {
		case "":
			// Generic chat continuation: reply, end turn, await next.
			emitAssistantText(fmt.Sprintf("Acknowledged: %s", truncateForLog(userText, 120)))
			emitSystemEnd()
		case "plan_complete":
			emitAssistantText("Plan approved. Calling plan_complete.")
			emitToolUse("plan_complete", planCompletePayload(cardID))
			emitSystemEnd()

			return
		case "review_approve":
			emitAssistantText("Review approved. Calling review_approve.")
			emitToolUse("review_approve", reviewApprovePayload(cardID))
			emitSystemEnd()

			return
		case "review_revise":
			emitAssistantText("Review wants changes. Calling review_revise.")
			emitToolUse("review_revise", reviewRevisePayload(cardID, userText))
			emitSystemEnd()

			return
		}
	}
}

// detectHITLDecision parses the user message for trigger words that
// drive the terminal marker. Empty string means "no decision yet, keep
// chatting".
func detectHITLDecision(p phase, userText string) string {
	lower := strings.ToLower(userText)

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
