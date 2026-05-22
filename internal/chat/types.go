package chat

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/binary"
	"time"

	"github.com/mhersson/contextmatrix/internal/chat/transcript"
)

// Status is the lifecycle state of a chat session.
type Status string

const (
	StatusCold     Status = "cold"
	StatusActive   Status = "active"
	StatusWarmIdle Status = "warm-idle"
	StatusEnding   Status = "ending"
)

func (s Status) String() string { return string(s) }

// ParseStatus reports whether s is a valid Status.
func ParseStatus(s string) (Status, bool) {
	switch Status(s) {
	case StatusCold, StatusActive, StatusWarmIdle, StatusEnding:
		return Status(s), true
	}

	return "", false
}

// Role is the kind of message in a transcript.
type Role string

const (
	RoleUser              Role = "user"
	RoleAssistantText     Role = "assistant_text"
	RoleAssistantThinking Role = "assistant_thinking"
	RoleToolCall          Role = "tool_call"
	RoleToolResult        Role = "tool_result"
	RoleUserQuestion      Role = "user_question"
	RoleStderr            Role = "stderr"
	RoleSystem            Role = "system"
)

// Session is the persisted shape of a chat session row.
type Session struct {
	ID                     string    `json:"id"`
	Title                  string    `json:"title"`
	Project                string    `json:"project,omitempty"`
	Status                 Status    `json:"status"`
	CreatedAt              time.Time `json:"created_at"`
	LastActive             time.Time `json:"last_active"`
	CreatedBy              string    `json:"created_by"`
	ContainerID            string    `json:"container_id,omitempty"`
	Workspace              []string  `json:"workspace,omitempty"`
	Model                  string    `json:"model,omitempty"`
	ContextTokens          int64     `json:"context_tokens,omitempty"`
	ContextTokensUpdatedAt time.Time `json:"context_tokens_updated_at"`
	RehydrationActive      bool      `json:"rehydration_active,omitempty"`
	// RehydrationStartedAt is set when SetRehydrationActive(true) is called and
	// cleared (nil) when called with false. The reaper compares this — not
	// LastActive — against the rehydration timeout so an actively-typing user
	// whose agent crashed mid-rehydration does not prevent the sweep from firing.
	RehydrationStartedAt *time.Time `json:"rehydration_started_at,omitempty"`

	// Token counters — cumulative totals from all usage frames for this session.
	PromptTokens        int64 `json:"prompt_tokens,omitempty"`
	CompletionTokens    int64 `json:"completion_tokens,omitempty"`
	CacheReadTokens     int64 `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int64 `json:"cache_creation_tokens,omitempty"`
	// EstimatedCostUSD is a float64 running total of the estimated USD cost
	// accumulated from all usage frames. Precision floor is ~$0.0001; migrate
	// to integer cents if sub-cent billing accuracy is ever required.
	EstimatedCostUSD float64 `json:"estimated_cost_usd,omitempty"`
}

// Message is a single persisted transcript entry. Kind discriminates
// structural markers from regular transcript rows: an empty Kind is a
// regular message; "divider" marks the system row appended on Clear
// Context so the frontend can render it as a horizontal rule both on
// the live SSE wire and after a page reload via the REST bootstrap.
type Message struct {
	ID               int64     `json:"id"`
	SessionID        string    `json:"session_id"`
	Seq              int64     `json:"seq"`
	Role             Role      `json:"role"`
	Content          string    `json:"content"` // JSON envelope, opaque to the store
	Kind             string    `json:"kind,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	RehydrationPhase bool      `json:"rehydration_phase,omitempty"`
}

// LogEntry is a parsed event from the runner's /logs SSE stream. The Type
// values mirror the runner's logbroadcast.LogEntry.Type vocabulary: "text",
// "thinking", "tool_call", "stderr", "system", "user", "usage". The chat
// package translates Type → Role when bridging into the transcript. "usage"
// entries are metadata (Claude stream-json usage block) and carry token
// counts in Usage; they do NOT become transcript entries.
type LogEntry struct {
	Timestamp time.Time
	Type      string
	Content   string
	Usage     *TokenUsage
	Model     string
}

// TokenUsage carries per-turn (per-assistant-message) token counts from the
// Anthropic Messages-API usage block emitted by the runner for each assistant
// turn. These are NOT cumulative session totals — each frame reports only the
// tokens consumed by that single turn. The sum of all four fields approximates
// the prompt size Claude actually processed; the UI typically displays
// InputTokens + CacheReadTokens + CacheCreateTokens as "context used.".
type TokenUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	CacheReadTokens   int64 `json:"cache_read_tokens"`
	CacheCreateTokens int64 `json:"cache_creation_tokens"`
}

// ResumeContext is the rehydration payload CM passes to the runner on a
// cold-open. The runner writes it to /run/cm-chat/resume.jsonl inside the
// container; the entrypoint instructs Claude to read it before greeting
// the operator. Defined in the transcript subpackage so its filtering
// logic stays free of import cycles; aliased here for the rest of the
// chat package and external callers.
type ResumeContext = transcript.ResumeContext

// ResumeTurn is one filtered, possibly summarized transcript entry in the
// rehydration payload.
type ResumeTurn = transcript.ResumeTurn

// NewID returns a 26-char ULID-shaped identifier (48-bit Unix-millis prefix
// + 80 random bits, encoded with the standard base32 alphabet). It is
// time-sortable but uses RFC4648 base32 (A-Z2-7), not Crockford.
func NewID() string {
	var b [16]byte

	// Encode the 48-bit Unix millisecond timestamp into the leading 6 bytes
	// in big-endian order. We build an 8-byte buffer and skip the top two
	// bytes (which are unused for timestamps well past Y2K).
	var tsBuf [8]byte
	binary.BigEndian.PutUint64(tsBuf[:], uint64(time.Now().UnixMilli()))
	copy(b[0:6], tsBuf[2:])

	_, _ = rand.Read(b[6:])
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)

	return enc.EncodeToString(b[:])[:26]
}
