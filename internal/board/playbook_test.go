package board

import (
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
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, SlugifyPlaybookTitle(tt.in), tt.in)
	}
}
