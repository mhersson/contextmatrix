package boardmerge

import (
	"strings"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pb() *board.Playbook {
	return &board.Playbook{
		ID: "release", Title: "Release", CreatedBy: "human:alice", NextEntryID: 3, Created: ts(0), Updated: ts(0),
		Entries: []board.PlaybookEntry{
			{ID: "e1", Type: board.EntryTypeCard, Project: "alpha", Card: "ALPHA-001"},
			{ID: "e2", Type: board.EntryTypeManual, Text: "sign off"},
		},
	}
}

func serializePb(t *testing.T, p *board.Playbook) []byte {
	t.Helper()

	b, err := board.SerializePlaybook(p)
	require.NoError(t, err)

	return b
}

func TestResolvePlaybook(t *testing.T) {
	base, ours, theirs := pb(), pb(), pb()
	ours.Entries = append(ours.Entries, board.PlaybookEntry{ID: "e3", Type: board.EntryTypeCard, Project: "alpha", Card: "ALPHA-009"})
	ours.NextEntryID, ours.Updated = 4, ts(5)

	theirs.Entries = append(theirs.Entries, board.PlaybookEntry{ID: "e3", Type: board.EntryTypeCard, Project: "alpha", Card: "ALPHA-002"})
	theirs.Entries[1].Done = true
	theirs.NextEntryID, theirs.Updated = 4, ts(2)
	c := testCtx()
	c.Renames = map[string]string{"alpha/ALPHA-009": "ALPHA-010"}

	out, err := Resolve(Input{
		Path: "playbooks/release.yaml", Base: serializePb(t, base),
		Ours: serializePb(t, ours), Theirs: serializePb(t, theirs),
	}, c)
	require.NoError(t, err)
	got, err := board.ParsePlaybook(out.Content)
	require.NoError(t, err)
	require.NoError(t, got.Validate())

	ids := []string{}
	for _, e := range got.Entries {
		ids = append(ids, e.ID)
	}

	assert.Equal(t, []string{"e1", "e2", "e3", "e4"}, ids)
	assert.True(t, got.Entries[1].Done)
	assert.Equal(t, "ALPHA-002", got.Entries[2].Card)
	assert.Equal(t, "ALPHA-010", got.Entries[3].Card) // ours re-minted and renamed
	assert.Equal(t, 5, got.NextEntryID)
	assert.Equal(t, ts(5), got.Updated)
	assert.Equal(t, "human:alice", got.CreatedBy)
	assert.Equal(t, ts(0), got.Created)

	require.NotEmpty(t, out.Resolutions)
	assert.Equal(t, RulePlaybookRemint, out.Resolutions[0].Rule)
	assert.Equal(t, "e3", out.Resolutions[0].OldID)
	assert.Equal(t, "e4", out.Resolutions[0].NewID)
}

func TestResolvePlaybook_AddAddReslugs(t *testing.T) {
	c := testCtx()
	c.PlaybookExists = func(id string) bool { return id == "release" }
	out, err := Resolve(Input{Path: "playbooks/release.yaml", Ours: serializePb(t, pb()), Theirs: serializePb(t, pb())}, c)
	require.NoError(t, err)
	require.Len(t, out.Extra, 1)
	assert.Equal(t, "playbooks/release-2.yaml", out.Extra[0].Path)

	got, err := board.ParsePlaybook(out.Extra[0].Content)
	require.NoError(t, err)
	require.NoError(t, got.Validate())
	assert.Equal(t, "release-2", got.ID)
	assert.Equal(t, RulePlaybookReslug, out.Resolutions[0].Rule)
	assert.Equal(t, "release", out.Resolutions[0].OldID)
	assert.Equal(t, "release-2", out.Resolutions[0].NewID)
}

func TestResolvePlaybook_AddAddReslugsPastTakenSlug(t *testing.T) {
	c := testCtx()
	taken := map[string]bool{"release": true, "release-2": true, "release-3": true}
	c.PlaybookExists = func(id string) bool { return taken[id] }
	out, err := Resolve(Input{Path: "playbooks/release.yaml", Ours: serializePb(t, pb()), Theirs: serializePb(t, pb())}, c)
	require.NoError(t, err)
	require.Len(t, out.Extra, 1)
	assert.Equal(t, "playbooks/release-4.yaml", out.Extra[0].Path)

	got, err := board.ParsePlaybook(out.Extra[0].Content)
	require.NoError(t, err)
	assert.Equal(t, "release-4", got.ID)
}

func TestResolvePlaybook_DuplicateCardEntryCollapsesToEarliest(t *testing.T) {
	base, ours, theirs := pb(), pb(), pb()
	ours.Entries = append(ours.Entries, board.PlaybookEntry{ID: "e3", Type: board.EntryTypeCard, Project: "alpha", Card: "ALPHA-005"})
	ours.NextEntryID = 4

	theirs.Entries = append(theirs.Entries, board.PlaybookEntry{ID: "e4", Type: board.EntryTypeCard, Project: "alpha", Card: "ALPHA-005"})
	theirs.NextEntryID = 4

	out, err := Resolve(Input{
		Path: "playbooks/release.yaml", Base: serializePb(t, base),
		Ours: serializePb(t, ours), Theirs: serializePb(t, theirs),
	}, testCtx())
	require.NoError(t, err)

	got, err := board.ParsePlaybook(out.Content)
	require.NoError(t, err)
	require.NoError(t, got.Validate())

	var matches int

	for _, e := range got.Entries {
		if e.Type == board.EntryTypeCard && e.Project == "alpha" && e.Card == "ALPHA-005" {
			matches++
		}
	}

	assert.Equal(t, 1, matches)
	assert.Equal(t, "e4", got.Entries[len(got.Entries)-1].ID) // the earlier (remote) entry survives

	var dedupe bool

	for _, r := range out.Resolutions {
		if r.Rule == RulePlaybookDedupe {
			dedupe = true
		}
	}

	assert.True(t, dedupe)
}

func TestResolvePlaybook_TitleAndDescriptionConflictPickLaterUpdated(t *testing.T) {
	base, ours, theirs := pb(), pb(), pb()
	ours.Title, ours.Description, ours.Updated = "Ours Title", "ours desc", ts(1)
	theirs.Title, theirs.Description, theirs.Updated = "Theirs Title", "theirs desc", ts(5)

	out, err := Resolve(Input{
		Path: "playbooks/release.yaml", Base: serializePb(t, base),
		Ours: serializePb(t, ours), Theirs: serializePb(t, theirs),
	}, testCtx())
	require.NoError(t, err)

	got, err := board.ParsePlaybook(out.Content)
	require.NoError(t, err)
	assert.Equal(t, "Theirs Title", got.Title)
	assert.Equal(t, "theirs desc", got.Description)

	var laterUpdated int

	for _, r := range out.Resolutions {
		if r.Rule == RuleLaterUpdated {
			laterUpdated++
		}
	}

	assert.Equal(t, 2, laterUpdated)
}

func TestResolvePlaybook_DeleteWins(t *testing.T) {
	base, theirs := pb(), pb()
	out, err := Resolve(Input{Path: "playbooks/release.yaml", Base: serializePb(t, base), Ours: nil, Theirs: serializePb(t, theirs)}, testCtx())
	require.NoError(t, err)
	assert.True(t, out.Deleted)
	require.Len(t, out.Resolutions, 1)
	assert.Equal(t, RuleDeleteWins, out.Resolutions[0].Rule)
}

func TestResolvePlaybook_Unparseable(t *testing.T) {
	theirs := pb()
	valid := serializePb(t, theirs)
	garbage := []byte("not: [valid")

	tests := []struct {
		name        string
		ours        []byte
		theirs      []byte
		wantContent []byte
	}{
		{"ours unparseable takes theirs", garbage, valid, valid},
		{"theirs unparseable takes ours", valid, garbage, valid},
		{"neither parses takes theirs", garbage, garbage, garbage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Resolve(Input{Path: "playbooks/release.yaml", Ours: tt.ours, Theirs: tt.theirs}, testCtx())
			require.NoError(t, err)
			assert.Equal(t, tt.wantContent, out.Content)
			require.Len(t, out.Resolutions, 1)
			assert.Equal(t, RuleUnparseable, out.Resolutions[0].Rule)
		})
	}
}

func TestResolvePlaybook_EntryRemovedByOursDropped(t *testing.T) {
	base, ours, theirs := pb(), pb(), pb()
	ours.Entries = ours.Entries[:1] // ours drops e2
	theirs.Entries[1].Text = "sign off (theirs edit)"

	out, err := Resolve(Input{
		Path: "playbooks/release.yaml", Base: serializePb(t, base),
		Ours: serializePb(t, ours), Theirs: serializePb(t, theirs),
	}, testCtx())
	require.NoError(t, err)

	got, err := board.ParsePlaybook(out.Content)
	require.NoError(t, err)
	require.NoError(t, got.Validate())

	ids := []string{}
	for _, e := range got.Entries {
		ids = append(ids, e.ID)
	}

	assert.Equal(t, []string{"e1"}, ids) // e2 stays gone even though theirs edited it
}

func TestResolvePlaybook_EntryRemovedByTheirsDropped(t *testing.T) {
	base, ours, theirs := pb(), pb(), pb()
	theirs.Entries = theirs.Entries[:1] // theirs drops e2
	ours.Entries[1].Done = true

	out, err := Resolve(Input{
		Path: "playbooks/release.yaml", Base: serializePb(t, base),
		Ours: serializePb(t, ours), Theirs: serializePb(t, theirs),
	}, testCtx())
	require.NoError(t, err)

	got, err := board.ParsePlaybook(out.Content)
	require.NoError(t, err)
	require.NoError(t, got.Validate())

	ids := []string{}
	for _, e := range got.Entries {
		ids = append(ids, e.ID)
	}

	assert.Equal(t, []string{"e1"}, ids) // e2 stays gone even though ours edited it
}

func TestResolvePlaybook_EntryOursOnlyEditKept(t *testing.T) {
	base, ours, theirs := pb(), pb(), pb()
	ours.Entries[1].Text = "sign off (ours edit)" // theirs matches base, unchanged

	out, err := Resolve(Input{
		Path: "playbooks/release.yaml", Base: serializePb(t, base),
		Ours: serializePb(t, ours), Theirs: serializePb(t, theirs),
	}, testCtx())
	require.NoError(t, err)

	got, err := board.ParsePlaybook(out.Content)
	require.NoError(t, err)
	require.NoError(t, got.Validate())
	assert.Equal(t, "sign off (ours edit)", got.Entries[1].Text)
}

func TestResolvePlaybook_EntryConflictPicksLaterUpdated(t *testing.T) {
	tests := []struct {
		name          string
		oursUpdated   time.Time
		theirsUpdated time.Time
		wantText      string
	}{
		{"theirs later wins", ts(1), ts(5), "theirs edit"},
		{"ours later wins", ts(5), ts(1), "ours edit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, ours, theirs := pb(), pb(), pb()
			ours.Entries[1].Text, ours.Updated = "ours edit", tt.oursUpdated
			theirs.Entries[1].Text, theirs.Updated = "theirs edit", tt.theirsUpdated

			out, err := Resolve(Input{
				Path: "playbooks/release.yaml", Base: serializePb(t, base),
				Ours: serializePb(t, ours), Theirs: serializePb(t, theirs),
			}, testCtx())
			require.NoError(t, err)

			got, err := board.ParsePlaybook(out.Content)
			require.NoError(t, err)
			require.NoError(t, got.Validate())
			assert.Equal(t, tt.wantText, got.Entries[1].Text)

			var found bool

			for _, r := range out.Resolutions {
				if r.Rule == RuleLaterUpdated && strings.Contains(r.Detail, "e2") {
					found = true
				}
			}

			assert.True(t, found, "expected a RuleLaterUpdated resolution naming entry e2")
		})
	}
}

func TestResolvePlaybook_RunFieldsPickLaterUpdated(t *testing.T) {
	base, ours, theirs := pb(), pb(), pb()
	base.Runnable = true

	ours.Runnable = true
	ours.BaseBranch = "main"
	ours.Run = &board.PlaybookRun{Status: board.RunStatusRunning, Instance: "lap-a", StartedAt: ts(1), UpdatedAt: ts(5), Entry: "e1"}
	ours.Updated = ts(5)

	theirs.Runnable = true
	theirs.BaseBranch = "develop"
	theirs.Run = &board.PlaybookRun{Status: board.RunStatusWaiting, Instance: "lap-b", StartedAt: ts(1), UpdatedAt: ts(2), Entry: "e1", Reason: "gate"}
	theirs.Updated = ts(2)

	out, err := Resolve(Input{
		Path: "playbooks/release.yaml", Base: serializePb(t, base),
		Ours: serializePb(t, ours), Theirs: serializePb(t, theirs),
	}, testCtx())
	require.NoError(t, err)
	got, err := board.ParsePlaybook(out.Content)
	require.NoError(t, err)
	require.NoError(t, got.Validate())

	assert.True(t, got.Runnable)
	assert.Equal(t, "main", got.BaseBranch)
	require.NotNil(t, got.Run)
	assert.Equal(t, board.RunStatusRunning, got.Run.Status)
	assert.Equal(t, "lap-a", got.Run.Instance)

	details := []string{}
	for _, r := range out.Resolutions {
		details = append(details, r.Detail)
	}

	assert.Contains(t, strings.Join(details, "\n"), "base_branch")
	assert.Contains(t, strings.Join(details, "\n"), "run")
}

func TestResolvePlaybook_OneSideSetsRunnable(t *testing.T) {
	base, ours, theirs := pb(), pb(), pb()
	theirs.Runnable = true
	theirs.BaseBranch = "main"
	theirs.Updated = ts(3)

	out, err := Resolve(Input{
		Path: "playbooks/release.yaml", Base: serializePb(t, base),
		Ours: serializePb(t, ours), Theirs: serializePb(t, theirs),
	}, testCtx())
	require.NoError(t, err)
	got, err := board.ParsePlaybook(out.Content)
	require.NoError(t, err)

	assert.True(t, got.Runnable)
	assert.Equal(t, "main", got.BaseBranch)
	assert.Nil(t, got.Run)
	assert.Empty(t, out.Resolutions)
}

func TestResolvePlaybook_RunDroppedWhenRunnableCleared(t *testing.T) {
	// Ours cleared runnable (a one-sided change, so it wins regardless of
	// timestamps) while theirs touched the run block (a both-sided change
	// that goes to the later side). Whichever side is later, the merged
	// result must not carry a run block on a non-runnable playbook.
	for _, tc := range []struct {
		name          string
		oursUpdated   time.Time
		theirsUpdated time.Time
	}{
		{name: "theirs later", oursUpdated: ts(2), theirsUpdated: ts(4)},
		{name: "ours later", oursUpdated: ts(9), theirsUpdated: ts(4)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, ours, theirs := pb(), pb(), pb()
			base.Runnable = true
			base.Run = &board.PlaybookRun{Status: board.RunStatusCompleted, StartedAt: ts(1), UpdatedAt: ts(1)}

			ours.Runnable = false
			ours.Run = nil
			ours.Updated = tc.oursUpdated

			theirs.Runnable = true
			theirs.Run = &board.PlaybookRun{Status: board.RunStatusCompleted, StartedAt: ts(1), UpdatedAt: ts(4), Reason: "note"}
			theirs.Updated = tc.theirsUpdated

			out, err := Resolve(Input{
				Path: "playbooks/release.yaml", Base: serializePb(t, base),
				Ours: serializePb(t, ours), Theirs: serializePb(t, theirs),
			}, testCtx())
			require.NoError(t, err)
			got, err := board.ParsePlaybook(out.Content)
			require.NoError(t, err)
			require.NoError(t, got.Validate())
			assert.False(t, got.Runnable, "one-sided clear wins regardless of timestamps")
			assert.Nil(t, got.Run)
		})
	}
}

func TestResolvePlaybook_OneSidedChangeSurvivesLaterPeer(t *testing.T) {
	base, ours, theirs := pb(), pb(), pb()
	ours.Runnable = true
	ours.BaseBranch = "main"
	ours.Updated = ts(1)

	// Theirs touched neither field but is later overall from an unrelated edit.
	theirs.Description = "touched"
	theirs.Updated = ts(10)

	out, err := Resolve(Input{
		Path: "playbooks/release.yaml", Base: serializePb(t, base),
		Ours: serializePb(t, ours), Theirs: serializePb(t, theirs),
	}, testCtx())
	require.NoError(t, err)
	got, err := board.ParsePlaybook(out.Content)
	require.NoError(t, err)

	assert.True(t, got.Runnable)
	assert.Equal(t, "main", got.BaseBranch)
	assert.Equal(t, "touched", got.Description)
	assert.Empty(t, out.Resolutions)
}

// runningPb is pb() made runnable with two card entries and a run on e1
// owned by lap-a.
func runningPb() *board.Playbook {
	p := pb()
	p.Runnable = true
	p.Entries = []board.PlaybookEntry{
		{ID: "e1", Type: board.EntryTypeCard, Project: "alpha", Card: "ALPHA-001"},
		{ID: "e2", Type: board.EntryTypeCard, Project: "alpha", Card: "ALPHA-002"},
	}
	p.Run = &board.PlaybookRun{Status: board.RunStatusRunning, Instance: "lap-a", StartedAt: ts(0), UpdatedAt: ts(0), Entry: "e1"}

	return p
}

func entryIDs(p *board.Playbook) []string {
	ids := make([]string, 0, len(p.Entries))
	for _, e := range p.Entries {
		ids = append(ids, e.ID)
	}

	return ids
}

func hasRule(res []Resolution, rule string) bool {
	for _, r := range res {
		if r.Rule == rule {
			return true
		}
	}

	return false
}

func resolveParsed(t *testing.T, base, ours, theirs *board.Playbook) (*board.Playbook, Output) {
	t.Helper()

	out, err := Resolve(Input{
		Path: "playbooks/release.yaml", Base: serializePb(t, base),
		Ours: serializePb(t, ours), Theirs: serializePb(t, theirs),
	}, testCtx())
	require.NoError(t, err)

	got, err := board.ParsePlaybook(out.Content)
	require.NoError(t, err)

	return got, out
}

// TestResolvePlaybook_ActiveRunKeepsItsCurrentEntry: the owner advanced the
// run to e2 while a stale peer removed e2. Removal wins the entry merge and
// the owner's run wins the run merge, so without the rule the run names no
// entry and every instance's index skips the playbook while its worker runs.
func TestResolvePlaybook_ActiveRunKeepsItsCurrentEntry(t *testing.T) {
	tests := []struct {
		name        string
		ownerIsOurs bool
		peerRun     *board.PlaybookRun // nil keeps the base run on the peer
	}{
		{name: "owner is ours", ownerIsOurs: true},
		{name: "owner is theirs", ownerIsOurs: false},
		{
			name: "both sides changed the run", ownerIsOurs: false,
			peerRun: &board.PlaybookRun{Status: board.RunStatusWaiting, Instance: "lap-a", StartedAt: ts(0), UpdatedAt: ts(2), Entry: "e1", Reason: "gate"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, owner, peer := runningPb(), runningPb(), runningPb()
			owner.Run = &board.PlaybookRun{Status: board.RunStatusRunning, Instance: "lap-a", StartedAt: ts(0), UpdatedAt: ts(5), Entry: "e2"}
			owner.Updated = ts(5)
			peer.Entries = peer.Entries[:1]
			peer.Updated = ts(3)

			if tt.peerRun != nil {
				peer.Run = tt.peerRun
			}

			ours, theirs := owner, peer
			if !tt.ownerIsOurs {
				ours, theirs = peer, owner
			}

			got, out := resolveParsed(t, base, ours, theirs)
			require.NoError(t, got.Validate(), "the merged playbook must load")
			assert.Equal(t, []string{"e1", "e2"}, entryIDs(got), "e2 is back at its base position")
			require.NotNil(t, got.Run)
			assert.Equal(t, "e2", got.Run.Entry)
			assert.True(t, hasRule(out.Resolutions, RulePlaybookRunEntry))
		})
	}
}

// TestResolvePlaybook_DedupeRepointsTheRunEntry: both sides added the same
// card under different ids and the run names the copy the dedupe drops. The
// run moves to the surviving entry for that card.
func TestResolvePlaybook_DedupeRepointsTheRunEntry(t *testing.T) {
	base := runningPb()
	base.Entries = base.Entries[:1]

	ours := runningPb()
	ours.Entries = []board.PlaybookEntry{base.Entries[0], {ID: "e3", Type: board.EntryTypeCard, Project: "alpha", Card: "ALPHA-005"}}
	ours.NextEntryID = 4
	ours.Run = &board.PlaybookRun{Status: board.RunStatusRunning, Instance: "lap-a", StartedAt: ts(0), UpdatedAt: ts(5), Entry: "e3"}
	ours.Updated = ts(5)

	theirs := runningPb()
	theirs.Entries = []board.PlaybookEntry{base.Entries[0], {ID: "e4", Type: board.EntryTypeCard, Project: "alpha", Card: "ALPHA-005"}}
	theirs.NextEntryID = 5
	theirs.Updated = ts(2)

	got, out := resolveParsed(t, base, ours, theirs)
	require.NoError(t, got.Validate())
	assert.Equal(t, []string{"e1", "e4"}, entryIDs(got))
	require.NotNil(t, got.Run)
	assert.Equal(t, "e4", got.Run.Entry, "the run follows its card to the surviving entry")
	assert.True(t, hasRule(out.Resolutions, RulePlaybookRunEntry))
}

// TestResolvePlaybook_InactiveRunEntryRemovedIsCleared: one side removed a
// stopped run's entry, the other side's later run block still names it.
// The removal stands and the dangling entry is cleared with its reason.
func TestResolvePlaybook_InactiveRunEntryRemovedIsCleared(t *testing.T) {
	stopped := func(updated int, entry, reason string) *board.PlaybookRun {
		return &board.PlaybookRun{Status: board.RunStatusStopped, Instance: "lap-a", StartedAt: ts(0), UpdatedAt: ts(updated), Entry: entry, Reason: reason}
	}

	base := runningPb()
	base.Run = stopped(0, "e2", "stopped by human:alice")

	ours := runningPb()
	ours.Entries = ours.Entries[:1]
	ours.Run = stopped(0, "", "")
	ours.Updated = ts(5)

	theirs := runningPb()
	theirs.Run = stopped(7, "e2", "stopped by human:bob")
	theirs.Updated = ts(7)

	got, out := resolveParsed(t, base, ours, theirs)
	require.NoError(t, got.Validate())
	assert.Equal(t, []string{"e1"}, entryIDs(got))
	require.NotNil(t, got.Run)
	assert.Empty(t, got.Run.Entry)
	assert.Empty(t, got.Run.Reason)
	assert.True(t, hasRule(out.Resolutions, RuleInvariantRepair))
}

// TestResolvePlaybook_InvalidMergeKeepsTheRemote: a merge result that still
// fails validation would be skipped on load; the remote version is kept.
func TestResolvePlaybook_InvalidMergeKeepsTheRemote(t *testing.T) {
	base, ours, theirs := pb(), pb(), pb()
	ours.Title, ours.Updated = " ", ts(5)
	theirs.Title, theirs.Updated = "Theirs Title", ts(2)

	theirsContent := serializePb(t, theirs)

	out, err := Resolve(Input{
		Path: "playbooks/release.yaml", Base: serializePb(t, base),
		Ours: serializePb(t, ours), Theirs: theirsContent,
	}, testCtx())
	require.NoError(t, err)
	assert.Equal(t, theirsContent, out.Content)
	assert.True(t, hasRule(out.Resolutions, RuleInvariantFallback))
}
