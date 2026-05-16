package chat

import (
	"context"
	"errors"
	"time"
)

// ErrSessionNotFound is returned when a session ID has no row.
var ErrSessionNotFound = errors.New("chat: session not found")

// Store persists chat sessions and messages. Implementations must be
// safe for concurrent use.
type Store interface {
	CreateSession(ctx context.Context, s Session) error
	GetSession(ctx context.Context, id string) (Session, error)
	ListSessions(ctx context.Context, filter SessionFilter) ([]Session, error)
	UpdateSession(ctx context.Context, s Session) error
	DeleteSession(ctx context.Context, id string) error

	// SetRehydrationActive flips the rehydration_active flag on a session
	// row without rewriting the rest of the columns. Returns
	// ErrSessionNotFound if no row matches.
	SetRehydrationActive(ctx context.Context, sessionID string, active bool) error

	// UpdateContextTokens stamps the context-window usage from the most
	// recent Claude turn onto the session row. updatedAt is the runner-side
	// timestamp of the usage event. Returns ErrSessionNotFound if no row
	// matches.
	UpdateContextTokens(ctx context.Context, sessionID string, tokens int64, updatedAt time.Time) error

	AppendMessage(ctx context.Context, m Message) (int64, error)
	ListMessages(ctx context.Context, sessionID string, sinceSeq int64, limit int) ([]Message, error)

	// MarkAllMessagesRehydrationPhase flips rehydration_phase = 1 on every
	// row for sessionID that is still at 0. Used by Manager.ClearContext to
	// exclude pre-clear messages from future rehydration payloads without
	// deleting them from the transcript. Returns the number of rows
	// updated (zero is not an error: clearing an empty session is valid).
	MarkAllMessagesRehydrationPhase(ctx context.Context, sessionID string) (int64, error)

	// ListMessagesTail returns the newest limit messages for sessionID in
	// chronological (ASC) order. Used by buildResume so rehydration payloads
	// reflect recent context rather than oldest. limit <= 0 returns nil.
	ListMessagesTail(ctx context.Context, sessionID string, limit int) ([]Message, error)

	// MaxSeq returns the largest seq for a session, or 0 if no messages exist.
	// Used by the Manager to seed monotonic seq assignment after restart
	// without scanning the full transcript.
	MaxSeq(ctx context.Context, sessionID string) (int64, error)

	Close() error
}

// SessionFilter narrows ListSessions.
type SessionFilter struct {
	Project   string
	Status    Status
	CreatedBy string
	// RehydrationActive, when non-nil, restricts results to rows where
	// rehydration_active matches the pointed value. Used by the reaper
	// to find sessions whose rehydration phase needs forcing off.
	RehydrationActive *bool
	// LastActiveBefore, when non-zero, restricts results to rows where
	// last_active is strictly older than this time. Used by the reaper
	// alongside RehydrationActive to find stale phases.
	LastActiveBefore time.Time
	// Limit, when > 0, caps the number of rows returned (ORDER BY
	// last_active DESC). Zero means no limit.
	Limit int
}
