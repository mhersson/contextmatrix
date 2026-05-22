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
	// row without rewriting the rest of the columns. When active is true,
	// rehydration_started_at is set to startedAt; when false it is cleared
	// to NULL. Returns ErrSessionNotFound if no row matches.
	SetRehydrationActive(ctx context.Context, sessionID string, active bool, startedAt time.Time) error

	// UpdateContextTokens stamps the context-window usage from the most
	// recent Claude turn onto the session row. updatedAt is the runner-side
	// timestamp of the usage event. Returns ErrSessionNotFound if no row
	// matches.
	UpdateContextTokens(ctx context.Context, sessionID string, tokens int64, updatedAt time.Time) error

	// IncrementSessionCost atomically adds the supplied token deltas and cost
	// to the session row using a single UPDATE … RETURNING statement. This is
	// race-free even under concurrent increments because the DB performs the
	// arithmetic, not the caller. Returns the new running totals. Returns
	// ErrSessionNotFound when no row matches (errors.Is(err, sql.ErrNoRows)).
	IncrementSessionCost(ctx context.Context, sessionID string,
		dPrompt, dCompletion, dCacheRead, dCacheCreation int64,
		dCost float64, model string,
	) (newPrompt, newCompletion, newCacheRead, newCacheCreation int64, newCost float64, err error)

	// UpdateSessionTitle writes only the title column without touching the
	// rest of the session row. Used by the AppendMessage auto-title path so
	// a concurrent OpenSession/MarkActive between the title-read and the
	// title-write cannot have its ContainerID/Status/Workspace overwritten
	// by a stale snapshot. Returns ErrSessionNotFound if no row matches.
	UpdateSessionTitle(ctx context.Context, sessionID, title string) error

	AppendMessage(ctx context.Context, m Message) (int64, error)
	ListMessages(ctx context.Context, sessionID string, sinceSeq int64, limit int) ([]Message, error)

	// ClearTranscriptAtomic marks all prior messages as rehydration_phase=1
	// and inserts the divider message in a single database transaction. If
	// either operation fails the transaction is rolled back, leaving the
	// transcript unchanged. Returns the number of rows marked plus the
	// inserted divider message (with its assigned ID).
	ClearTranscriptAtomic(ctx context.Context, sessionID string, divider Message) (markedCount int64, inserted Message, err error)

	// ListMessagesTail returns the newest limit messages for sessionID in
	// chronological (ASC) order. Used by buildResume so rehydration payloads
	// reflect recent context rather than oldest. limit <= 0 returns nil.
	ListMessagesTail(ctx context.Context, sessionID string, limit int) ([]Message, error)

	// MaxSeq returns the largest seq for a session, or 0 if no messages exist.
	// Used by the Manager to seed monotonic seq assignment after restart
	// without scanning the full transcript.
	MaxSeq(ctx context.Context, sessionID string) (int64, error)

	// CountSessionsByStatus returns the number of sessions whose status is
	// one of the supplied values. Used by openCold to enforce MaxConcurrent
	// without fetching full rows just for len().
	CountSessionsByStatus(ctx context.Context, statuses ...Status) (int, error)

	// AggregateCost returns cost aggregates over the supplied time windows.
	// last30d is the sum of estimated_cost_usd for sessions whose last_active
	// falls in [since, until). prior30d covers the prior window of equal
	// width ending at since. series30d is a length-30 daily slice (index 0 =
	// since, index 29 = the day before until), filled with 0.0 for days with
	// no sessions. The caller is responsible for aligning since/until to UTC
	// midnight boundaries; no boundary adjustment is performed here.
	AggregateCost(ctx context.Context, since, until time.Time) (last30d, prior30d float64, series30d []float64, err error)

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
	// last_active is strictly older than this time.
	LastActiveBefore time.Time
	// RehydrationStartedBefore, when non-zero, restricts results to rows
	// where rehydration_started_at is strictly older than this time. Used by
	// the reaper instead of LastActiveBefore to detect stale rehydration
	// phases without being fooled by user typing that keeps LastActive fresh.
	RehydrationStartedBefore time.Time
	// Limit, when > 0, caps the number of rows returned (ORDER BY
	// last_active DESC). Zero means no limit.
	Limit int
}
