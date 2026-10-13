package jiraimport

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/jira"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCardTitle_NeutralisesSmartCommits(t *testing.T) {
	tests := []struct {
		name, summary, want string
	}{
		{"plain", "Fix login", "PROJ-1 Fix login"},
		{"command", "Fix #close the flow", "PROJ-1 Fix close the flow"},
		{"run of hashes", "Fix ##close", "PROJ-1 Fix close"},
		{"digit", "Revert #123", "PROJ-1 Revert 123"},
		{"two commands", "a #done#close", "PROJ-1 a doneclose"},
		{"lone hash kept", "  Support C# and F# ", "PROJ-1 Support C# and F#"},
		{"hash before space kept", "Use # sign", "PROJ-1 Use # sign"},
		{"non-ascii letter", "Fix #ærlig", "PROJ-1 Fix ærlig"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cardTitle(jira.Issue{Key: "PROJ-1", Summary: tt.summary})
			assert.Equal(t, tt.want, got)
			assert.NotRegexp(t, `#[\p{L}\p{N}]`, got)
		})
	}
}

func TestCardType(t *testing.T) {
	full := []string{"task", "bug", "feature"}

	tests := []struct {
		name  string
		it    jira.IssueType
		types []string
		want  string
	}{
		{"bug", jira.IssueType{Name: "Bug"}, full, "bug"},
		{"story", jira.IssueType{Name: "Story"}, full, "task"},
		{"task", jira.IssueType{Name: "Task"}, full, "task"},
		{"improvement", jira.IssueType{Name: "Improvement"}, full, "feature"},
		{"new feature without feature type", jira.IssueType{Name: "New Feature"}, []string{"task", "bug"}, "task"},
		{"sub-task", jira.IssueType{Name: "Sub-task", HierarchyLevel: -1, Subtask: true}, full, "task"},
		{"renamed sub-task level", jira.IssueType{Name: "Checklist item", HierarchyLevel: -1}, full, "task"},
		{"custom type", jira.IssueType{Name: "Spike"}, full, "task"},
		{"project lacks task", jira.IssueType{Name: "Story"}, []string{"chore", "bug"}, "chore"},
		{"subtask listed first is skipped", jira.IssueType{Name: "Story"}, []string{"subtask", "chore"}, "chore"},
		{"bug without bug type", jira.IssueType{Name: "Bug"}, []string{"task"}, "task"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cardType(tt.it, tt.types)
			assert.Equal(t, tt.want, got)
			assert.NotEqual(t, board.SubtaskType, got)
		})
	}
}

func TestCardPriority(t *testing.T) {
	full := []string{"low", "medium", "high", "critical"}
	noCritical := []string{"low", "medium", "high"}

	tests := []struct {
		name, jiraPriority string
		priorities         []string
		want               string
	}{
		{"highest", "Highest", full, "critical"},
		{"blocker", "Blocker", full, "critical"},
		{"high", "High", full, "high"},
		{"normal", "Normal", full, "medium"},
		{"minor", "Minor", full, "low"},
		{"trivial", "trivial", full, "low"},
		{"unset", "", full, "medium"},
		{"unknown", "P0-urgent", full, "medium"},
		{"critical falls back to high", "Highest", noCritical, "high"},
		{"no shared names", "High", []string{"p1", "p2"}, "p1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cardPriority(tt.jiraPriority, tt.priorities))
		})
	}
}

func TestCardBody_EpicContext(t *testing.T) {
	url := "https://acme.atlassian.net/browse/PROJ-1"
	epic := jira.Issue{Key: "PROJ-1", Description: adfDoc("Epic goal")}
	issue := jira.Issue{Key: "PROJ-2", Description: adfDoc("Story text")}

	section := epicSection(epic, url)
	require.NotEmpty(t, section)
	assert.Contains(t, section, "Epic goal")
	assert.Contains(t, section, "("+url+")")

	body := cardBody(issue, section)
	assert.Less(t, strings.Index(body, "Story text"), strings.Index(body, "Epic goal"), "the issue text comes first")

	assert.NotContains(t, cardBody(issue, ""), "Epic goal")
	assert.Empty(t, epicSection(jira.Issue{Key: "PROJ-1"}, url), "an empty epic description adds no section")
	assert.Equal(t, section, cardBody(jira.Issue{Key: "PROJ-3"}, section), "an issue without a description carries only the section")
}

func TestCardInput(t *testing.T) {
	svc := New(nil, nil, nil, Config{SiteURL: "https://acme.atlassian.net/"})
	cfg := &board.ProjectConfig{Types: []string{"task", "bug"}, Priorities: []string{"low", "medium", "high"}}
	issue := jira.Issue{
		ID: "102", Key: "PROJ-2", Summary: "Cart #close API", Priority: "High",
		IssueType: jira.IssueType{Name: "Story"}, Description: adfDoc("Story text"),
	}
	epic := jira.Issue{Key: "PROJ-1", Description: adfDoc("Epic goal")}

	without := svc.cardInput(issue, cfg, "", false, "human:alice")
	with := svc.cardInput(issue, cfg, epicSection(epic, svc.browseURL("PROJ-1")), true, "human:alice")

	assert.Equal(t, "PROJ-2 Cart close API", with.Title)
	assert.Equal(t, "task", with.Type)
	assert.Equal(t, "high", with.Priority)
	assert.Empty(t, with.Parent)
	assert.Equal(t, &board.Source{System: jira.SourceSystem, ExternalID: "PROJ-2", ExternalURL: "https://acme.atlassian.net/browse/PROJ-2"}, with.Source)
	assert.True(t, with.Vetted)
	assert.False(t, without.Vetted)
	assert.Equal(t, "102", with.Custom[jira.CustomIssueID])
	require.NotNil(t, with.Log)
	assert.Equal(t, "human:alice", with.Log.Agent)

	// jira_import_hash is a contract a future refresh reads: hex SHA-256 of
	// title + "\n" + body, epic section included.
	sum := sha256.Sum256([]byte(with.Title + "\n" + with.Body))
	assert.Equal(t, hex.EncodeToString(sum[:]), with.Custom[jira.CustomImportHash])
	assert.Equal(t, importHash(without.Title, without.Body), without.Custom[jira.CustomImportHash])
	assert.NotEqual(t, without.Custom[jira.CustomImportHash], with.Custom[jira.CustomImportHash])
}
