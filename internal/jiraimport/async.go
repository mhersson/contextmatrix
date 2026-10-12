package jiraimport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"time"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
)

// Background import states. ImportFailed covers an import that could not
// resolve its playbook and one stopped by shutdown or its time bound; the
// selections it never reached have no result.
const (
	ImportRunning = "running"
	ImportDone    = "done"
	ImportFailed  = "failed"
)

const (
	// importTimeout bounds one background import.
	importTimeout = 10 * time.Minute
	// importRetention is how long a finished import stays readable.
	importRetention = time.Hour
)

// ImportState is one background import as GET /api/jira/imports/{id} shows it.
type ImportState struct {
	ID         string     `json:"id"`
	Epic       string     `json:"epic"`
	Status     string     `json:"status"`
	PlaybookID string     `json:"playbook_id,omitempty"`
	Total      int        `json:"total"`
	Results    []Result   `json:"results"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// Start sets the context background imports run under. Cancelling it, or
// Shutdown, stops each running import between two issues.
func (s *Service) Start(ctx context.Context) {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	s.base, s.stop = context.WithCancel(ctx)
}

// Begin reads the epic tree on the request's context, then imports in the
// background and returns the import's id. An error means nothing was written.
func (s *Service) Begin(ctx context.Context, epicKey string, req ImportRequest, agentID string) (string, error) {
	t, err := s.prepare(ctx, epicKey, req)
	if err != nil {
		return "", err
	}

	id, err := newImportID()
	if err != nil {
		return "", err
	}

	now := s.clk.Now().UTC()

	s.runMu.Lock()

	// Checked under runMu, which Shutdown cancels under, so no wg.Add can
	// race the final wg.Wait.
	base := s.base
	if base == nil || base.Err() != nil {
		s.runMu.Unlock()

		return "", ErrNotAccepting
	}

	s.pruneLocked(now)
	s.imports[id] = &ImportState{
		ID: id, Epic: t.epic.Key, Status: ImportRunning,
		Total: distinctSelections(req), Results: []Result{}, StartedAt: now,
	}
	s.wg.Add(1)
	s.runMu.Unlock()

	go s.run(base, id, t, req, agentID)

	return id, nil
}

// distinctSelections counts the issues an import covers; a key selected
// twice is imported once.
func distinctSelections(req ImportRequest) int {
	seen := map[string]bool{}
	for _, sel := range req.Selections {
		seen[sel.Key] = true
	}

	return len(seen)
}

func (s *Service) run(base context.Context, id string, t *tree, req ImportRequest, agentID string) {
	defer s.wg.Done()

	ctx, cancel := context.WithTimeout(base, s.timeout)
	defer cancel()

	var (
		resp *ImportResponse
		err  error
	)

	func() {
		// A panic must end this import, not the server.
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("jira import panicked: %v", p)
			}
		}()

		resp, err = s.importTree(ctx, t, req, agentID, func(r Result) { s.progress(id, agentID, r) })
	}()

	s.finish(ctx, id, agentID, resp, err)
}

func (s *Service) progress(id, agentID string, r Result) {
	s.runMu.Lock()
	if st, ok := s.imports[id]; ok {
		st.Results = append(st.Results, r)
	}
	s.runMu.Unlock()

	s.publish(events.JiraImportProgress, agentID, map[string]any{
		"import_id": id, "key": r.Key, "status": r.Status, "project": r.Project, "card_id": r.Card, "reason": r.Reason,
	})
}

// finish records the outcome. Error and the done event carry failureReason's
// fixed phrase, never the raw error, which can hold paths and remote URLs.
func (s *Service) finish(ctx context.Context, id, agentID string, resp *ImportResponse, err error) {
	now := s.clk.Now().UTC()

	s.runMu.Lock()

	st := s.imports[id]
	st.Status = ImportDone
	st.FinishedAt = &now

	if resp != nil {
		st.PlaybookID = resp.Playbook.ID
		st.Results = append([]Result{}, resp.Results...)
	}

	if err != nil {
		ctxlog.Logger(ctx).Warn("jira import stopped", "import", id, "epic", st.Epic, "error", err)

		st.Status = ImportFailed
		st.Error = failureReason(err)
	}

	data := map[string]any{
		"import_id": id, "status": st.Status, "playbook_id": st.PlaybookID,
		"results": slices.Clone(st.Results), "error": st.Error,
	}

	s.runMu.Unlock()

	s.publish(events.JiraImportDone, agentID, data)
}

func (s *Service) publish(t events.EventType, agentID string, data map[string]any) {
	if s.cfg.Bus == nil {
		return
	}

	s.cfg.Bus.Publish(events.Event{Type: t, Agent: agentID, Timestamp: s.clk.Now().UTC(), Data: data})
}

// Lookup returns a copy of an import's state; false for an unknown id or an
// import that finished more than an hour ago. The registry is in memory, so
// a restart forgets every import.
func (s *Service) Lookup(id string) (*ImportState, bool) {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	s.pruneLocked(s.clk.Now().UTC())

	st, ok := s.imports[id]
	if !ok {
		return nil, false
	}

	cp := *st
	cp.Results = slices.Clone(st.Results)

	if st.FinishedAt != nil {
		finished := *st.FinishedAt
		cp.FinishedAt = &finished
	}

	return &cp, true
}

func (s *Service) pruneLocked(now time.Time) {
	for id, st := range s.imports {
		if st.FinishedAt != nil && now.Sub(*st.FinishedAt) > importRetention {
			delete(s.imports, id)
		}
	}
}

// Shutdown stops running imports between two issues and waits for them,
// bounded by ctx. The issue in flight gets drainGrace to finish its writes
// before they are cancelled; Begin refuses from now on.
func (s *Service) Shutdown(ctx context.Context) error {
	s.runMu.Lock()
	if s.stop != nil {
		s.stop()
	}
	s.runMu.Unlock()

	done := make(chan struct{})

	go func() {
		defer close(done)

		s.wg.Wait()
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newImportID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("import id: %w", err)
	}

	return "ji-" + hex.EncodeToString(b), nil
}
