package lock

import (
	"context"
	"fmt"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// foreignLease is what this instance last saw of a claim another instance
// holds: the pushed lease value and the local time that value first appeared.
// Only the local clock is ever compared, so peer clock skew cannot stall a
// live card.
type foreignLease struct {
	value     time.Time
	epoch     int
	firstSeen time.Time
}

func leaseKey(project, id string) string { return project + "/" + id }

// SetShared names this instance and sets the lease timings. Until it is
// called the manager behaves as on a private board: agent-ID ownership, no
// claim fields written, no lease tables.
func (m *Manager) SetShared(instance string, leaseInterval, leaseTimeout time.Duration) {
	m.instance = instance
	m.leaseInterval = leaseInterval
	m.leaseTimeout = leaseTimeout
}

// Instance returns the instance ID, empty on a private board.
func (m *Manager) Instance() string { return m.instance }

func (m *Manager) recordBeat(project, id string, at time.Time) {
	m.leaseMu.Lock()
	defer m.leaseMu.Unlock()

	m.beats[leaseKey(project, id)] = at
}

func (m *Manager) confirm(project, id string, at time.Time) {
	m.leaseMu.Lock()
	defer m.leaseMu.Unlock()

	m.confirmed[leaseKey(project, id)] = at
}

// ClearBeat forgets the live beat and the confirmation stamp of a card this
// instance no longer holds.
func (m *Manager) ClearBeat(project, id string) {
	m.leaseMu.Lock()
	defer m.leaseMu.Unlock()

	delete(m.beats, leaseKey(project, id))
	delete(m.confirmed, leaseKey(project, id))
}

// LastBeat returns the newer of the card's file heartbeat and the live beat
// this instance holds for it. The live beat is liveness of a claim this
// instance is running, so it is consulted only while the card still carries
// that claim: an unclaimed card, a card a peer took over, and a claim from
// before shared boards all report the file value, as does any card this
// instance never beat. Callers can use it for every card.
func (m *Manager) LastBeat(card *board.Card) *time.Time {
	if card.AssignedAgent == "" || card.ClaimedVia != m.instance {
		return card.LastHeartbeat
	}

	m.leaseMu.Lock()
	live, ok := m.beats[leaseKey(card.Project, card.ID)]
	m.leaseMu.Unlock()

	if !ok || (card.LastHeartbeat != nil && !live.After(*card.LastHeartbeat)) {
		return card.LastHeartbeat
	}

	return &live
}

// ObserveLeases records, for every card another instance holds, the lease
// value on file and the local time it was first seen with that value. Called
// after every reload of the index.
func (m *Manager) ObserveLeases(ctx context.Context) error {
	if m.instance == "" {
		return nil
	}

	projects, err := m.store.ListProjects(ctx)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	now := m.clk.Now()
	seen := map[string]bool{}

	m.leaseMu.Lock()
	defer m.leaseMu.Unlock()

	for _, p := range projects {
		cards, err := m.store.ListCards(ctx, p.Name, storage.CardFilter{})
		if err != nil {
			return fmt.Errorf("list cards for project %s: %w", p.Name, err)
		}

		for _, c := range cards {
			if !c.ClaimedElsewhere(m.instance) {
				continue
			}

			k := leaseKey(p.Name, c.ID)
			seen[k] = true

			value := leaseValue(c)
			if cur, ok := m.foreign[k]; ok && cur.value.Equal(value) && cur.epoch == c.ClaimEpoch {
				continue
			}

			m.foreign[k] = foreignLease{value: value, epoch: c.ClaimEpoch, firstSeen: now}
		}
	}

	for k := range m.foreign {
		if !seen[k] {
			delete(m.foreign, k)
		}
	}

	return nil
}

func leaseValue(c *board.Card) time.Time {
	if c.LastHeartbeat == nil {
		return time.Time{}
	}

	return *c.LastHeartbeat
}

// ForeignLeaseExpired reports whether a card another instance holds has kept
// the same lease value, at the same epoch, for longer than lease_timeout on
// this instance's clock.
func (m *Manager) ForeignLeaseExpired(card *board.Card) bool {
	if !card.ClaimedElsewhere(m.instance) {
		return false
	}

	m.leaseMu.Lock()
	defer m.leaseMu.Unlock()

	cur, ok := m.foreign[leaseKey(card.Project, card.ID)]
	if !ok || !cur.value.Equal(leaseValue(card)) || cur.epoch != card.ClaimEpoch {
		return false
	}

	return m.clk.Now().Sub(cur.firstSeen) > m.leaseTimeout
}

// ConfirmLeases stamps every claim this instance holds as confirmed on the
// remote. Called after a successful sync cycle, when the remote holds every
// local commit and therefore the lease value peers judge by.
func (m *Manager) ConfirmLeases(ctx context.Context) error {
	if m.instance == "" {
		return nil
	}

	projects, err := m.store.ListProjects(ctx)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	now := m.clk.Now()

	m.leaseMu.Lock()
	defer m.leaseMu.Unlock()

	for _, p := range projects {
		cards, err := m.store.ListCards(ctx, p.Name, storage.CardFilter{})
		if err != nil {
			return fmt.Errorf("list cards for project %s: %w", p.Name, err)
		}

		for _, c := range cards {
			if c.AssignedAgent != "" && c.ClaimedVia == m.instance {
				m.confirmed[leaseKey(p.Name, c.ID)] = now
			}
		}
	}

	return nil
}

// Fenced reports whether a claim this instance holds has gone unconfirmed on
// the remote for longer than lease_timeout, so a peer may already have
// stalled or taken over the card. Legacy claims carry no lease and are never
// fenced.
func (m *Manager) Fenced(card *board.Card) bool {
	if m.instance == "" || card.AssignedAgent == "" || card.ClaimedVia != m.instance {
		return false
	}

	m.leaseMu.Lock()
	defer m.leaseMu.Unlock()

	at, ok := m.confirmed[leaseKey(card.Project, card.ID)]

	return !ok || m.clk.Now().Sub(at) > m.leaseTimeout
}
