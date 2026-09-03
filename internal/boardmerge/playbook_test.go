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
