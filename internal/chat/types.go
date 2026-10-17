package chat

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/binary"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
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
	RoleStderr            Role = "stderr"
	RoleSystem            Role = "system"
)

// SessionKind selects the backend a session runs on and what it binds to.
type SessionKind string

const (
	// KindChat is a chat session: one optional project, a worker container
	// on the chat backend.
	KindChat SessionKind = "chat"
	// KindArchitect is a blueprint session on the architect backend: its
	// target projects live in Workspace, its blueprint id is the session id,
	// and it runs no container.
	KindArchitect SessionKind = "architect"
)

// Session is the persisted shape of a chat session row.
type Session struct {
	ID      string      `json:"id"`
	Title   string      `json:"title"`
	Project string      `json:"project,omitempty"`
	Kind    SessionKind `json:"kind"`
	// Mode is the architect autonomy mode (protocol.ArchitectMode*); empty
	// for chat sessions. Fixed at creation.
	Mode string `json:"mode,omitempty"`
	// CostCapUSD bounds the architect's spend for this blueprint; zero means
	// the architect's own default. Fixed at creation.
	CostCapUSD             float64   `json:"cost_cap_usd,omitempty"`
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
	// cleared (nil) when called with false. The reaper compares this - not
	// LastActive - against the rehydration timeout so an actively-typing user
	// whose agent crashed mid-rehydration does not prevent the sweep from firing.
	RehydrationStartedAt *time.Time `json:"rehydration_started_at,omitempty"`

	// Token counters - cumulative totals from all usage frames for this session.
	PromptTokens        int64 `json:"prompt_tokens,omitempty"`
	CompletionTokens    int64 `json:"completion_tokens,omitempty"`
	CacheReadTokens     int64 `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int64 `json:"cache_creation_tokens,omitempty"`
	// EstimatedCostUSD is a float64 running total of the estimated USD cost
	// accumulated from all usage frames. Precision floor is ~$0.0001; migrate
	// to integer cents if sub-cent billing accuracy is ever required.
	EstimatedCostUSD float64 `json:"estimated_cost_usd,omitempty"`

	// AssistantWorking reports whether a turn is currently in flight for
	// this session. In-memory only - populated by Manager.GetSession and
	// ListSessions from the working map, never persisted by the store.
	AssistantWorking bool `json:"assistant_working,omitempty"`
	// AssistantWorkingSince is the UTC start of the in-flight turn. Nil when
	// AssistantWorking is false. Late-joining browsers use it to show a
	// truthful elapsed time.
	AssistantWorkingSince *time.Time `json:"assistant_working_since,omitempty"`
}

// Message is a single persisted transcript entry. Kind discriminates
// structural rows from regular ones: empty is a regular message, "divider"
// marks the Clear Context row, and "prompt" marks an architect question or
// gate whose Content is a {text, prompt} JSON envelope, and "marker" marks an architect
// stage divider or sheet reference whose Content is a {text, marker} JSON envelope. Kind rides the live
// SSE wire and the REST bootstrap alike, so the browser renders both the same.
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

// LogEntry is a parsed event from the backend's /logs SSE stream. The Type
// values mirror the backend's logbroadcast.LogEntry.Type vocabulary: "text",
// "thinking", "tool_call", "stderr", "system", "user", "usage",
// "tool_result", "status". The chat package translates Type → Role when
// bridging into the transcript. "usage" entries are metadata (the
// backend's per-turn usage report) and carry token counts in Usage; they do NOT
// become transcript entries. "status" frames carry run-state
// ("working"/"idle") and never become transcript entries.
type LogEntry struct {
	Timestamp time.Time
	Type      string
	Content   string
	Usage     *TokenUsage
	Model     string
	// Prompt is set on an architect text frame that offers reply buttons.
	Prompt *protocol.LogPrompt
	// Marker is set on an architect frame that renders as a stage divider or
	// a sheet reference.
	Marker *protocol.LogMarker
}

// TokenUsage carries per-turn (per-assistant-message) token counts reported
// by the backend on its log stream for each assistant turn. These are NOT
// cumulative session totals - each frame reports only the
// tokens consumed by that single turn. The sum of all four fields approximates
// the prompt size the model actually processed; the UI typically displays
// InputTokens + CacheReadTokens + CacheCreateTokens as "context used.".
type TokenUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	CacheReadTokens   int64 `json:"cache_read_tokens"`
	CacheCreateTokens int64 `json:"cache_creation_tokens"`
}

// ResumeContext is the rehydration payload CM passes to the backend on a
// cold-open. The backend writes it to /run/cm-chat/resume.jsonl inside the
// container; the worker loads it and seeds it as conversation history before
// the first turn. Protocol-owned wire shape (protocol.ChatResumeContext),
// aliased via the transcript subpackage so the builder and the rest of
// the chat package keep their local names.
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
