package jira

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsIssueKey(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"PROJ-1", true},
		{"AB_C2-907", true},
		{"proj-1", false},
		{"P-1", false},
		{"PROJ-0", false},
		{"PROJ-012", false},
		{"PROJ1", false},
		{"PROJ-1 ", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, IsIssueKey(tt.in))
		})
	}
}

func TestParseIssueRef(t *testing.T) {
	const site = "https://acme.atlassian.net"

	tests := []struct {
		name    string
		input   string
		site    string
		want    string
		wantErr bool
	}{
		{name: "key", input: "PROJ-123", site: site, want: "PROJ-123"},
		{name: "lowercase key is upper-cased", input: " proj-123 ", site: site, want: "PROJ-123"},
		{name: "browse URL", input: "https://acme.atlassian.net/browse/PROJ-7", site: site, want: "PROJ-7"},
		{name: "browse URL trailing slash", input: "https://acme.atlassian.net/browse/PROJ-7/", site: site, want: "PROJ-7"},
		{name: "host case-insensitive", input: "https://ACME.atlassian.net/browse/PROJ-7", site: site, want: "PROJ-7"},
		{name: "site configured with trailing slash", input: "https://acme.atlassian.net/browse/PROJ-7", site: site + "/", want: "PROJ-7"},
		{
			name:  "selectedIssue query",
			input: "https://acme.atlassian.net/jira/software/projects/PROJ/boards/1?selectedIssue=PROJ-42",
			site:  site, want: "PROJ-42",
		},
		{name: "foreign host", input: "https://evil.example/browse/PROJ-7", site: site, wantErr: true},
		{name: "http scheme", input: "http://acme.atlassian.net/browse/PROJ-7", site: site, wantErr: true},
		{name: "site URL naming no issue", input: "https://acme.atlassian.net/jira/your-work", site: site, wantErr: true},
		{name: "free text", input: "Name this playbook", site: site, wantErr: true},
		{name: "empty", input: "", site: site, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseIssueRef(tt.input, tt.site)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidRef)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
