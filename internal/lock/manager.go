// Package lock provides agent claim/release/heartbeat validation logic.
// The Lock Manager reads cards via the store to check ownership but does NOT write
// to the store. The caller (Service Layer) handles store writes, git commits,
// and event publishing.
package lock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// Sentinel errors for lock operations.
var (
	// ErrAlreadyClaimed is returned when attempting to claim a card that is
	// already assigned to another agent.
	ErrAlreadyClaimed = errors.New("card already claimed by another agent")

	// ErrNotClaimed is returned when attempting to release or heartbeat
	// a card that is not claimed.
	ErrNotClaimed = errors.New("card is not claimed")

	// ErrAgentMismatch is returned when the requesting agent does not match
	// the card's assigned agent.
	ErrAgentMismatch = errors.New("agent does not own this card")
)

// StalledCard represents a card that has exceeded the heartbeat timeout.
type StalledCard struct {
	Project string
	Card    *board.Card
}

// Manager handles agent claim/release/heartbeat operations.
// It validates ownership but does not persist changes—the caller must
// save the returned card via the Store.
type Manager struct {
	store   storage.Store
	timeout time.Duration
	clk     clock.Clock
}

// NewManager creates a lock manager with the given store and heartbeat timeout.
// Uses clock.Real() as the time source; for tests use NewManagerWithClock.
func NewManager(store storage.Store, timeout time.Duration) *Manager {
	return NewManagerWithClock(store, timeout, clock.Real())
}

// NewManagerWithClock is like NewManager but lets the caller inject a clock.
// Used by tests to deterministically advance heartbeat cutoffs.
func NewManagerWithClock(store storage.Store, timeout time.Duration, clk clock.Clock) *Manager {
	if clk == nil {
		clk = clock.Real()
	}

	return &Manager{
		store:   store,
		timeout: timeout,
		clk:     clk,
	}
}

// Claim attempts to assign a card to an agent. If the card is already claimed
// by another agent, ErrAlreadyClaimed is returned. On success, returns the
// modified card with assigned_agent and last_heartbeat set. The caller must
// persist the card.
func (m *Manager) Claim(ctx context.Context, project, cardID, agentID string) (*board.Card, error) {
	card, err := m.store.GetCard(ctx, project, cardID)
	if err != nil {
		return nil, fmt.Errorf("get card: %w", err)
	}

	if card.AssignedAgent != "" && card.AssignedAgent != agentID {
		return nil, fmt.Errorf("%w: currently held by %s", ErrAlreadyClaimed, card.AssignedAgent)
	}

	// If same agent is re-claiming, just update heartbeat
	now := m.clk.Now()
	card.AssignedAgent = agentID
	card.LastHeartbeat = &now
	card.Updated = now

	return card, nil
}

// Release removes an agent's claim on a card. If the card is not claimed,
// ErrNotClaimed is returned. If the card is claimed by a different agent,
// ErrAgentMismatch is returned. On success, returns the modified card with
// assigned_agent and last_heartbeat cleared. The caller must persist the card.
func (m *Manager) Release(ctx context.Context, project, cardID, agentID string) (*board.Card, error) {
	card, err := m.store.GetCard(ctx, project, cardID)
	if err != nil {
		return nil, fmt.Errorf("get card: %w", err)
	}

	if card.AssignedAgent == "" {
		return nil, ErrNotClaimed
	}

	if card.AssignedAgent != agentID {
		return nil, fmt.Errorf("%w: card is held by %s", ErrAgentMismatch, card.AssignedAgent)
	}

	card.AssignedAgent = ""
	card.LastHeartbeat = nil
	card.Updated = m.clk.Now()

	return card, nil
}

// Heartbeat updates the last_heartbeat timestamp for a claimed card.
// If the card is not claimed, ErrNotClaimed is returned. If the card is
// claimed by a different agent, ErrAgentMismatch is returned. On success,
// returns the modified card. The caller must persist the card.
func (m *Manager) Heartbeat(ctx context.Context, project, cardID, agentID string) (*board.Card, error) {
	card, err := m.store.GetCard(ctx, project, cardID)
	if err != nil {
		return nil, fmt.Errorf("get card: %w", err)
	}

	if card.AssignedAgent == "" {
		return nil, ErrNotClaimed
	}

	if card.AssignedAgent != agentID {
		return nil, fmt.Errorf("%w: card is held by %s", ErrAgentMismatch, card.AssignedAgent)
	}

	now := m.clk.Now()
	card.LastHeartbeat = &now
	card.Updated = now

	return card, nil
}

// FindStalled returns all cards across all projects where:
// - assigned_agent is set (card is claimed)
// - last_heartbeat is older than the configured timeout
//
// This method does NOT modify the cards. The caller (Service Layer)
// is responsible for transitioning stalled cards to the "stalled" state,
// clearing the agent, persisting changes, and publishing events.
func (m *Manager) FindStalled(ctx context.Context) ([]StalledCard, error) {
	projects, err := m.store.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	cutoff := m.clk.Now().Add(-m.timeout)

	var stalled []StalledCard

	for _, proj := range projects {
		// Only get claimed cards
		cards, err := m.store.ListCards(ctx, proj.Name, storage.CardFilter{})
		if err != nil {
			return nil, fmt.Errorf("list cards for project %s: %w", proj.Name, err)
		}

		for _, card := range cards {
			if card.AssignedAgent == "" {
				continue // Not claimed
			}

			if card.LastHeartbeat == nil {
				// Claimed but no heartbeat—treat as stalled
				stalled = append(stalled, StalledCard{
					Project: proj.Name,
					Card:    card,
				})

				continue
			}

			if card.LastHeartbeat.Before(cutoff) {
				stalled = append(stalled, StalledCard{
					Project: proj.Name,
					Card:    card,
				})
			}
		}
	}

	return stalled, nil
}

// Timeout returns the configured heartbeat timeout duration.
func (m *Manager) Timeout() time.Duration {
	return m.timeout
}

// Clock returns the clock the manager uses for Now() / cutoff comparisons.
// Exposed so callers (notably the service layer) can share the same clock
// source — important in tests where a fake clock is injected.
func (m *Manager) Clock() clock.Clock {
	return m.clk
}
