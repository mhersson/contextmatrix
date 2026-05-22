// Package transcript builds the rehydration payload sent to the runner on a
// cold-reopen. It filters role-typed messages from SQLite into the bounded
// shape Claude can ingest from /run/cm-chat/resume.jsonl: drop noise (thinking,
// stderr, system, prior rehydration turns), summarise tool_result bodies, and
// truncate to fit a configurable token budget while always preserving the
// first user turn (the original goal) and the last K turns (recent context).
//
// The package is intentionally free of dependencies on the surrounding chat
// package — Build operates on its own Message type so callers can convert
// once and unit-test the filtering rules in isolation.
package transcript

import "strings"

const (
	// DefaultBudgetTokens is the fallback when BuildOpts.BudgetTokens is zero.
	DefaultBudgetTokens = 40000

	// MaxTurns is the absolute upper bound on turns in a single resume.
	// Acts as a hard cap so a runaway transcript never produces an
	// unbounded payload even when the budget would allow more.
	MaxTurns = 500

	// MaxContentBytes caps each ResumeTurn.Content. The transcript pkg
	// is the last line of defence; manager.go already caps persisted
	// content at 32KB, but a 32KB tool_call line is still wasteful for
	// rehydration purposes.
	MaxContentBytes = 4 * 1024

	// AlwaysKeepLastK ensures the most recent turns survive truncation
	// so the agent always sees how the conversation actually ended.
	AlwaysKeepLastK = 20

	// truncationMarker is appended to ResumeTurn.Content when it exceeds
	// MaxContentBytes. Suffix is part of the cap.
	truncationMarker = " … [truncated]"
)

// Role string constants used by Message inputs and ResumeTurn outputs.
const (
	RoleUser              = "user"
	RoleAssistantText     = "assistant_text"
	RoleAssistantThinking = "assistant_thinking"
	RoleToolCall          = "tool_call"
	RoleToolResult        = "tool_result"
	RoleToolResultSummary = "tool_result_summary"
	RoleUserQuestion      = "user_question"
	RoleStderr            = "stderr"
	RoleSystem            = "system"
)

// Message is one persisted transcript entry in the input to Build. It
// mirrors the load-bearing fields of chat.Message; callers convert their
// type into this one before invoking Build.
type Message struct {
	Seq              int64
	Role             string
	Content          string
	RehydrationPhase bool
}

// ResumeContext is the rehydration payload CM passes to the runner on a
// cold-open. The runner writes it to /run/cm-chat/resume.jsonl inside the
// container; the entrypoint instructs Claude to read it before greeting
// the operator.
type ResumeContext struct {
	Turns   []ResumeTurn `json:"turns"`
	Clipped bool         `json:"clipped"`
	OrigSeq int64        `json:"original_seq"`
}

// ResumeTurn is one filtered, possibly summarized transcript entry in the
// rehydration payload. Roles: "user", "assistant_text", "tool_call",
// "user_question", "tool_result_summary" (tool_result bodies are collapsed
// to a one-liner outcome by the transcript builder).
type ResumeTurn struct {
	Seq     int64  `json:"seq"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

// BuildOpts carries the knobs the manager passes from config.
type BuildOpts struct {
	// BudgetTokens caps the rough token-count estimate of the produced
	// payload. Zero means use DefaultBudgetTokens.
	BudgetTokens int
}

// Build assembles a ResumeContext from a chronological transcript slice.
// Returns nil when there is nothing worth resuming (empty input or every
// message filtered out). Caller is expected to treat nil as "skip the
// rehydration path; start a fresh agent.".
func Build(msgs []Message, opts BuildOpts) *ResumeContext {
	if len(msgs) == 0 {
		return nil
	}

	budget := opts.BudgetTokens
	if budget <= 0 {
		budget = DefaultBudgetTokens
	}

	origSeq := msgs[len(msgs)-1].Seq

	turns := make([]ResumeTurn, 0, len(msgs))

	for _, m := range msgs {
		turn, ok := filterMessage(m)
		if !ok {
			continue
		}

		turns = append(turns, turn)
	}

	if len(turns) == 0 {
		return nil
	}

	clipped := false

	turns, hardClipped := applyHardTurnCap(turns)
	if hardClipped {
		clipped = true
	}

	turns, budgetClipped := applyBudget(turns, budget)
	if budgetClipped {
		clipped = true
	}

	return &ResumeContext{
		Turns:   turns,
		Clipped: clipped,
		OrigSeq: origSeq,
	}
}

// filterMessage maps one persisted Message to a ResumeTurn, or skips it.
// Roles dropped: assistant_thinking, stderr, system. Rehydration-phase
// messages are always dropped (anti-pollution on the 2nd+ reopen).
func filterMessage(m Message) (ResumeTurn, bool) {
	if m.RehydrationPhase {
		return ResumeTurn{}, false
	}

	switch m.Role {
	case RoleUser, RoleAssistantText, RoleToolCall, RoleUserQuestion:
		return ResumeTurn{
			Seq:     m.Seq,
			Role:    m.Role,
			Content: capContent(m.Content),
		}, true

	case RoleToolResult:
		return ResumeTurn{
			Seq:     m.Seq,
			Role:    RoleToolResultSummary,
			Content: summarizeToolResult(m.Content),
		}, true
	}

	return ResumeTurn{}, false
}

// applyHardTurnCap enforces MaxTurns by preserving the first user turn (if
// any) and the most recent MaxTurns-1 turns.
func applyHardTurnCap(turns []ResumeTurn) ([]ResumeTurn, bool) {
	if len(turns) <= MaxTurns {
		return turns, false
	}

	firstUserIdx := indexOfFirstUser(turns)
	tailStart := len(turns) - MaxTurns
	// Reserve one slot at the front for the first user turn so we can
	// always include it.
	if firstUserIdx >= 0 && firstUserIdx < tailStart {
		tailStart = len(turns) - (MaxTurns - 1)

		return append([]ResumeTurn{turns[firstUserIdx]}, turns[tailStart:]...), true
	}

	// No user turn in the early section, or the first user turn is
	// already inside the kept tail — just take the tail.
	return turns[tailStart:], true
}

// applyBudget drops oldest "middle" turns until the rough token estimate
// fits within budget. The first user turn and the last AlwaysKeepLastK
// turns are pinned. If the pinned set alone exceeds budget, we accept
// that — never refuse to build.
func applyBudget(turns []ResumeTurn, budget int) ([]ResumeTurn, bool) {
	if budget <= 0 {
		return turns, false
	}

	total := 0
	for _, t := range turns {
		total += estimateTokens(t.Content)
	}

	if total <= budget {
		return turns, false
	}

	firstUserIdx := indexOfFirstUser(turns)

	keepLastFrom := max(len(turns)-AlwaysKeepLastK, 0)

	dropped := make(map[int]bool)

	for i := range turns {
		if total <= budget {
			break
		}

		if i == firstUserIdx {
			continue
		}

		if i >= keepLastFrom {
			break // we've hit the always-kept tail; stop dropping.
		}

		dropped[i] = true
		total -= estimateTokens(turns[i].Content)
	}

	if len(dropped) == 0 {
		return turns, false
	}

	out := make([]ResumeTurn, 0, len(turns)-len(dropped))

	for i, t := range turns {
		if dropped[i] {
			continue
		}

		out = append(out, t)
	}

	return out, true
}

// indexOfFirstUser returns the position of the first "user" role turn, or -1
// if there is none.
func indexOfFirstUser(turns []ResumeTurn) int {
	for i, t := range turns {
		if t.Role == RoleUser {
			return i
		}
	}

	return -1
}

// estimateTokens returns a rough token count for a content string. Anthropic
// tokens average ~4 bytes for English prose; we use ceil-divide so empty
// strings stay at zero but a single character counts as one token.
func estimateTokens(s string) int {
	if s == "" {
		return 0
	}

	return (len(s) + 3) / 4
}

// capContent enforces the per-turn MaxContentBytes cap, appending the
// truncation marker. Truncation respects UTF-8 rune boundaries so the
// marker is not glued onto a partial multi-byte sequence.
func capContent(s string) string {
	if len(s) <= MaxContentBytes {
		return s
	}

	cut := max(MaxContentBytes-len(truncationMarker), 0)

	// Back up to a rune start.
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}

	return s[:cut] + truncationMarker
}

// summarizeToolResult collapses a tool_result body into a one-liner. The
// agent does not need to re-see the original payload — it can re-Read /
// re-run the producing tool_call if the content matters. The summary
// preserves the load-bearing signal (success vs. failure) and the tail
// of any error text.
func summarizeToolResult(content string) string {
	s := strings.TrimSpace(content)
	if s == "" {
		return "→ ok"
	}

	if looksLikeError(s) {
		tail := s
		if len(tail) > 200 {
			tail = tail[len(tail)-200:]
		}

		return "→ failed: " + tail
	}

	return "→ ok"
}

// looksLikeError heuristically classifies a tool_result body as a failure.
// We err on the side of "ok" — a noisy success that happens to contain the
// word "error" is preferable to mis-labelling a clean success as failure.
func looksLikeError(s string) bool {
	lower := strings.ToLower(s)
	for _, needle := range []string{
		"error:", "fatal:", "exit code 1", "exit code 2", "exit code 3",
		"exit status 1", "exit status 2", "exit status 3",
		"permission denied", "not found", "no such file",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}

	return false
}
