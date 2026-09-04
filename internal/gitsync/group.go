package gitsync

import (
	"context"
	"errors"
	"fmt"

	"github.com/mhersson/contextmatrix/internal/storage"
)

// GroupEntry is one boards repository in a Group. Syncer is nil when the
// repo has no remote; its status then reports sync disabled.
type GroupEntry struct {
	Name   string
	Syncer *Syncer
}

// Group fronts the syncers of every configured boards repository for the
// API: one status per repo in config order, and a manual trigger for one
// repo or all of them.
type Group struct {
	entries []GroupEntry
	hidden  func() []storage.HiddenProject
}

// NewGroup builds the group. hidden reports the projects the composite
// store hides; nil means none.
func NewGroup(hidden func() []storage.HiddenProject, entries ...GroupEntry) *Group {
	if hidden == nil {
		hidden = func() []storage.HiddenProject { return nil }
	}

	return &Group{entries: entries, hidden: hidden}
}

// Enabled reports whether any repo has a syncer.
func (g *Group) Enabled() bool {
	for _, e := range g.entries {
		if e.Syncer != nil {
			return true
		}
	}

	return false
}

// TriggerSync runs a manual sync of the named repo, or of every enabled
// repo when repo is empty. An unknown name is storage.ErrUnknownRepo; a
// repo without a remote, or no enabled repo at all, is ErrSyncDisabled.
func (g *Group) TriggerSync(ctx context.Context, repo string) error {
	if repo != "" {
		for _, e := range g.entries {
			if e.Name != repo {
				continue
			}

			if e.Syncer == nil {
				return fmt.Errorf("%s: %w", repo, ErrSyncDisabled)
			}

			return e.Syncer.TriggerSync(ctx)
		}

		return fmt.Errorf("%w: %q", storage.ErrUnknownRepo, repo)
	}

	if !g.Enabled() {
		return ErrSyncDisabled
	}

	var errs []error

	for _, e := range g.entries {
		if e.Syncer == nil {
			continue
		}

		if err := e.Syncer.TriggerSync(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name, err))
		}
	}

	return errors.Join(errs...)
}

// Statuses returns one status per repo in config order.
func (g *Group) Statuses() []SyncStatus {
	hidden := map[string][]string{}
	for _, h := range g.hidden() {
		hidden[h.Repo] = append(hidden[h.Repo], h.Name)
	}

	out := make([]SyncStatus, 0, len(g.entries))

	for _, e := range g.entries {
		var st SyncStatus
		if e.Syncer != nil {
			st = e.Syncer.Status()
		}

		st.Repo = e.Name
		st.HiddenProjects = hidden[e.Name]
		out = append(out, st)
	}

	return out
}

// Start launches every syncer's background loops.
func (g *Group) Start(ctx context.Context) {
	for _, e := range g.entries {
		if e.Syncer != nil {
			e.Syncer.Start(ctx)
		}
	}
}

// Wait blocks until every syncer's background loops have stopped.
func (g *Group) Wait() {
	for _, e := range g.entries {
		if e.Syncer != nil {
			e.Syncer.Wait()
		}
	}
}
