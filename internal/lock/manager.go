// Package lock provides agent claim/release/heartbeat validation logic.
// The Lock Manager reads cards via the store to check ownership but does NOT write
// to the store. The caller (Service Layer) handles store writes, git commits,
// and event publishing.
package lock

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
// It validates ownership but does not persist changes - the caller must
// save the returned card via the Store.
type Manager struct {
	store   storage.Store
	timeout time.Duration
	clk     clock.Clock

	// Shared-board state; empty instance means a private board.
	instance      string
	leaseInterval time.Duration
	leaseTimeout  time.Duration

	// leaseMu guards the three tables. It is never held while calling the
	// store, except in ObserveLeases and ConfirmLeases, which take it for
	// the whole scan so a concurrent beat cannot interleave with a rebuild.
	leaseMu   sync.Mutex
	beats     map[string]time.Time    // live heartbeats of claims this instance holds
	foreign   map[string]foreignLease // what this instance last saw of other instances' claims
	confirmed map[string]time.Time    // when each own claim was last confirmed on the remote
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
		store:     store,
		timeout:   timeout,
		clk:       clk,
		beats:     map[string]time.Time{},
		foreign:   map[string]foreignLease{},
		confirmed: map[string]time.Time{},
	}
}

// Claim assigns a card to an agent. On a private board a card held by another
// agent is ErrAlreadyClaimed and a re-claim by the holder refreshes the
// heartbeat. On a shared board ownership is the (agent, instance) pair: the
// same agent ID claiming through another instance is refused while that
// instance's lease is live, and takes the card over with a higher epoch once
// the lease has expired. The caller must persist the card.
func (m *Manager) Claim(ctx context.Context, project, cardID, agentID string) (*board.Card, error) {
	card, err := m.store.GetCard(ctx, project, cardID)
	if err != nil {
		return nil, fmt.Errorf("get card: %w", err)
	}

	now := m.clk.Now()

	if m.instance == "" {
		if card.AssignedAgent != "" && card.AssignedAgent != agentID {
			return nil, fmt.Errorf("%w: currently held by %s", ErrAlreadyClaimed, card.AssignedAgent)
		}

		card.AssignedAgent = agentID
		card.LastHeartbeat = &now
		card.Updated = now

		return card, nil
	}

	switch {
	case card.ClaimHeldBy(agentID, m.instance):
		// A refresh by the holder is liveness, not a new claim: the epoch
		// stays so a peer's later stall or takeover still wins the merge.
		card.LastHeartbeat = &now
		card.Updated = now
		m.recordBeat(project, cardID, now)

		return card, nil
	case card.AssignedAgent == "":
	case card.ClaimedElsewhere(m.instance):
		if !m.ForeignLeaseExpired(card) {
			return nil, fmt.Errorf("%w: currently held by %s via %s", ErrAlreadyClaimed, card.AssignedAgent, card.ClaimedVia)
		}
		// An expired lease is taken over below like a fresh claim.
	default:
		return nil, fmt.Errorf("%w: currently held by %s", ErrAlreadyClaimed, card.AssignedAgent)
	}

	card.AssignedAgent = agentID
	card.ClaimedVia = m.instance
	card.ClaimedAt = &now
	card.ClaimEpoch++
	card.LastHeartbeat = &now
	card.Updated = now

	m.recordBeat(project, cardID, now)
	m.confirm(project, cardID, now)

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

	if !card.ClaimHeldBy(agentID, m.instance) {
		return nil, fmt.Errorf("%w: card is held by %s%s", ErrAgentMismatch, card.AssignedAgent, viaSuffix(card))
	}

	m.dropClaim(card, project, cardID)

	return card, nil
}

// ForceRelease clears a card's claim regardless of which agent holds it.
// Human-operator path for crashed or wedged workers. Returns the modified
// card and the agent that previously held the claim so the caller can
// audit-log it. If the card is not claimed, ErrNotClaimed is returned.
// The caller must persist the card.
func (m *Manager) ForceRelease(ctx context.Context, project, cardID string) (*board.Card, string, error) {
	card, err := m.store.GetCard(ctx, project, cardID)
	if err != nil {
		return nil, "", fmt.Errorf("get card: %w", err)
	}

	if card.AssignedAgent == "" {
		return nil, "", ErrNotClaimed
	}

	prevAgent := card.AssignedAgent
	m.dropClaim(card, project, cardID)

	return card, prevAgent, nil
}

// dropClaim clears the tuple, bumps the epoch on a shared board, and forgets
// the live beat. It is the one place a claim ends.
func (m *Manager) dropClaim(card *board.Card, project, cardID string) {
	card.ClearClaim()
	card.Updated = m.clk.Now()

	if m.instance != "" {
		card.ClaimEpoch++

		m.ClearBeat(project, cardID)
	}
}

func viaSuffix(card *board.Card) string {
	if card.ClaimedVia == "" {
		return ""
	}

	return " via " + card.ClaimedVia
}

// Heartbeat records liveness for a claimed card. persist reports whether the
// returned card carries a new last_heartbeat the caller must write: always on
// a private board; on a shared board only when the value on file is older
// than lease_interval, so the file (and the remote) sees a renewal at most
// once per interval while the live beat stays in memory.
func (m *Manager) Heartbeat(ctx context.Context, project, cardID, agentID string) (*board.Card, bool, error) {
	card, err := m.store.GetCard(ctx, project, cardID)
	if err != nil {
		return nil, false, fmt.Errorf("get card: %w", err)
	}

	if card.AssignedAgent == "" {
		return nil, false, ErrNotClaimed
	}

	if !card.ClaimHeldBy(agentID, m.instance) {
		return nil, false, fmt.Errorf("%w: card is held by %s%s", ErrAgentMismatch, card.AssignedAgent, viaSuffix(card))
	}

	now := m.clk.Now()

	if m.instance != "" {
		m.recordBeat(project, cardID, now)

		if card.LastHeartbeat != nil && now.Sub(*card.LastHeartbeat) < m.leaseInterval {
			return card, false, nil
		}
	}

	card.LastHeartbeat = &now
	card.Updated = now

	return card, true, nil
}

// FindStalled returns all cards across all projects where:
//   - assigned_agent is set (card is claimed)
//   - the card is claimed via this instance (or with no claimed_via), and
//     neither the file heartbeat nor the live beat is newer than the timeout
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
			if card.AssignedAgent == "" || card.ClaimedElsewhere(m.instance) {
				continue // unclaimed, or another instance's to stall
			}

			last := m.LastBeat(card)
			if last == nil || last.Before(cutoff) {
				stalled = append(stalled, StalledCard{Project: proj.Name, Card: card})
			}
		}
	}

	return stalled, nil
}

func (m *Manager) Timeout() time.Duration {
	return m.timeout
}

// Clock returns the clock the manager uses for Now() / cutoff comparisons.
// Exposed so callers (notably the service layer) can share the same clock
// source - important in tests where a fake clock is injected.
func (m *Manager) Clock() clock.Clock {
	return m.clk
}
