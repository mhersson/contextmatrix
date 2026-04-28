// Package main is the contextmatrix integration-harness fake-claude CLI.
package main

import (
	"flag"
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
	_ = inputFormat
	_ = outputFormat

	if systemPrompt == "" {
		log.Fatalf("stub-claude: missing --append-system-prompt")
	}

	p := detectPhase(systemPrompt)
	if p == phaseUnknown {
		log.Fatalf("stub-claude: could not detect phase from system prompt; first 200 chars: %.200s", systemPrompt)
	}

	log.Printf("stub-claude: detected phase=%s model=%s", p, model)

	// Card ID is set by the runner via env var (CM_CARD_ID); use as
	// reference in the canned plan payload subtask description.
	cardID := os.Getenv("CM_CARD_ID")
	emitMarker(p, cardID)
}

// detectPhase scans the prompt for marker names. Each phase's prompt
// instructs the agent to emit the corresponding marker at end of phase,
// so the marker name is unambiguous in the prompt content.
func detectPhase(prompt string) phase {
	switch {
	case strings.Contains(prompt, "PLAN_DRAFTED"):
		return phasePlan
	case strings.Contains(prompt, "DOCS_WRITTEN"):
		return phaseDocs
	case strings.Contains(prompt, "REVIEW_FINDINGS"):
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
