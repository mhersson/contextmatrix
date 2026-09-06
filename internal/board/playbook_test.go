package board

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validPlaybook() *Playbook {
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)

	return &Playbook{
		ID: "alpha-rollout", Title: "Alpha rollout",
		CreatedBy: "human:alice", Created: now, Updated: now,
		NextEntryID: 3,
		Entries: []PlaybookEntry{
			{ID: "e1", Type: EntryTypeCard, Project: "project-alpha", Card: "ALPHA-101", Note: "merge first"},
			{ID: "e2", Type: EntryTypeManual, Text: "rebuild worker image", Done: true, DoneBy: "human:alice", DoneAt: &now},
		},
	}
}

func TestPlaybook_SerializeParseRoundTrip(t *testing.T) {
	p := validPlaybook()
	data, err := SerializePlaybook(p)
	require.NoError(t, err)

	got, err := ParsePlaybook(data)
	require.NoError(t, err)
	assert.Equal(t, p, got)
}

func TestParsePlaybook_IgnoresUnknownFields(t *testing.T) {
	// Lenient parsing: a future field (e.g. stage) must not break older binaries.
	data := []byte("id: x\ntitle: X\nnext_entry_id: 1\nstage_mode: linear\nentries:\n  - id: e1\n    type: manual\n    text: do it\n    stage: 2\n")
	p, err := ParsePlaybook(data)
	require.NoError(t, err)
	assert.Equal(t, "x", p.ID)
	assert.Len(t, p.Entries, 1)
}

func TestParsePlaybook_Malformed(t *testing.T) {
	_, err := ParsePlaybook([]byte("id: [unclosed"))
	assert.Error(t, err)
}

func TestPlaybook_Validate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Playbook)
		wantOK bool
	}{
		{"valid", func(*Playbook) {}, true},
		{"empty entries valid", func(p *Playbook) { p.Entries = nil }, true},
		{"missing title", func(p *Playbook) { p.Title = "" }, false},
		{"bad id shape", func(p *Playbook) { p.ID = "Bad_ID" }, false},
		{"card entry missing project", func(p *Playbook) { p.Entries[0].Project = "" }, false},
		{"card entry missing card", func(p *Playbook) { p.Entries[0].Card = "" }, false},
		{"card entry with text", func(p *Playbook) { p.Entries[0].Text = "nope" }, false},
		{"manual entry missing text", func(p *Playbook) { p.Entries[1].Text = "" }, false},
		{"manual entry with card ref", func(p *Playbook) { p.Entries[1].Card = "ALPHA-1" }, false},
		{"unknown entry type", func(p *Playbook) { p.Entries[0].Type = "webhook" }, false},
		{"duplicate card pair", func(p *Playbook) {
			p.Entries = append(p.Entries, PlaybookEntry{ID: "e3", Type: EntryTypeCard, Project: "project-alpha", Card: "ALPHA-101"})
		}, false},
		{"duplicate entry id", func(p *Playbook) {
			p.Entries = append(p.Entries, PlaybookEntry{ID: "e1", Type: EntryTypeManual, Text: "x"})
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validPlaybook()
			tt.mutate(p)

			err := p.Validate()
			if tt.wantOK {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, ErrInvalidPlaybook)
			}
		})
	}
}

func TestPlaybook_FindEntry(t *testing.T) {
	p := validPlaybook()
	assert.Equal(t, 1, p.FindEntry("e2"))
	assert.Equal(t, -1, p.FindEntry("e99"))
}

func TestPlaybook_HasCardEntry(t *testing.T) {
	p := validPlaybook()
	assert.True(t, p.HasCardEntry("project-alpha", "ALPHA-101"))
	assert.False(t, p.HasCardEntry("project-alpha", "ALPHA-999"))
}

func TestSlugifyPlaybookTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Alpha rollout", "alpha-rollout"},
		{"  Worker Image!! Rollout  ", "worker-image-rollout"},
		{"___", "playbook"},
		{"", "playbook"},
		{strings.Repeat("x", 250), strings.Repeat("x", 100)},
		// Truncation lands exactly on a hyphen; it must be trimmed too.
		{strings.Repeat("a ", 60), strings.Repeat("a-", 49) + "a"},
	}
	for _, tt := range tests {
		got := SlugifyPlaybookTitle(tt.in)
		assert.Equal(t, tt.want, got, tt.in)
		assert.LessOrEqual(t, len(got), maxSlugLength, tt.in)
		assert.Regexp(t, playbookIDPattern, got, tt.in)
	}
}

func TestPlaybook_BranchAndRunActive(t *testing.T) {
	p := &Playbook{ID: "alpha-rollout", Title: "Alpha"}
	assert.Equal(t, "playbook/alpha-rollout", p.Branch())
	assert.False(t, p.RunActive())

	p.Run = &PlaybookRun{Status: RunStatusRunning}
	assert.True(t, p.RunActive())

	p.Run.Status = RunStatusWaiting
	assert.True(t, p.RunActive())

	p.Run.Status = RunStatusStopped
	assert.False(t, p.RunActive())

	p.Run.Status = RunStatusCompleted
	assert.False(t, p.RunActive())

	var nilRun *PlaybookRun
	assert.False(t, nilRun.Active())
}

func TestPlaybook_ValidateRun(t *testing.T) {
	now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)

	base := func() *Playbook {
		return &Playbook{
			ID: "rollout", Title: "Rollout", Runnable: true, NextEntryID: 2,
			Entries: []PlaybookEntry{{ID: "e1", Type: EntryTypeCard, Project: "alpha", Card: "ALPHA-001"}},
		}
	}

	t.Run("valid run block", func(t *testing.T) {
		p := base()
		p.Run = &PlaybookRun{Status: RunStatusRunning, StartedAt: now, UpdatedAt: now, Entry: "e1"}
		require.NoError(t, p.Validate())
	})

	t.Run("unknown status rejected", func(t *testing.T) {
		p := base()
		p.Run = &PlaybookRun{Status: "paused", StartedAt: now, UpdatedAt: now}
		err := p.Validate()
		require.ErrorIs(t, err, ErrInvalidPlaybook)
		assert.Contains(t, err.Error(), "run status")
	})

	t.Run("run without runnable rejected", func(t *testing.T) {
		p := base()
		p.Runnable = false
		p.Run = &PlaybookRun{Status: RunStatusCompleted, StartedAt: now, UpdatedAt: now}
		err := p.Validate()
		require.ErrorIs(t, err, ErrInvalidPlaybook)
		assert.Contains(t, err.Error(), "runnable")
	})

	t.Run("entry must exist", func(t *testing.T) {
		p := base()
		p.Run = &PlaybookRun{Status: RunStatusWaiting, StartedAt: now, UpdatedAt: now, Entry: "e9"}
		err := p.Validate()
		require.ErrorIs(t, err, ErrInvalidPlaybook)
		assert.Contains(t, err.Error(), "e9")
	})

	t.Run("empty entry allowed", func(t *testing.T) {
		p := base()
		p.Run = &PlaybookRun{Status: RunStatusCompleted, StartedAt: now, UpdatedAt: now}
		require.NoError(t, p.Validate())
	})
}

func TestPlaybook_RunFieldsRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	ended := now.Add(time.Hour)
	p := &Playbook{
		ID: "rollout", Title: "Rollout", Runnable: true, BaseBranch: "main", NextEntryID: 1,
		Run: &PlaybookRun{
			Status: RunStatusStopped, Instance: "lap-a", StartedBy: "human:alice",
			StartedAt: now, UpdatedAt: now, Entry: "", Reason: "stopped by human", EndedAt: &ended,
		},
	}

	data, err := SerializePlaybook(p)
	require.NoError(t, err)
	assert.Contains(t, string(data), "runnable: true")
	assert.Contains(t, string(data), "base_branch: main")

	got, err := ParsePlaybook(data)
	require.NoError(t, err)
	assert.True(t, got.Runnable)
	assert.Equal(t, "main", got.BaseBranch)
	require.NotNil(t, got.Run)
	assert.Equal(t, RunStatusStopped, got.Run.Status)
	assert.Equal(t, "lap-a", got.Run.Instance)
	assert.Equal(t, ended, *got.Run.EndedAt)
}

func TestPlaybook_V1FileParsesWithoutRunFields(t *testing.T) {
	got, err := ParsePlaybook([]byte("id: old\ntitle: Old\nnext_entry_id: 1\nentries: []\n"))
	require.NoError(t, err)
	assert.False(t, got.Runnable)
	assert.Empty(t, got.BaseBranch)
	assert.Nil(t, got.Run)
	require.NoError(t, got.Validate())
}
