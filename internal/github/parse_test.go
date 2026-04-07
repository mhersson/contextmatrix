package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseGitHubRepo(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		{
			name:      "SSH format",
			url:       "git@github.com:myorg/myrepo.git",
			wantOwner: "myorg",
			wantRepo:  "myrepo",
			wantOK:    true,
		},
		{
			name:      "HTTPS with .git",
			url:       "https://github.com/myorg/myrepo.git",
			wantOwner: "myorg",
			wantRepo:  "myrepo",
			wantOK:    true,
		},
		{
			name:      "HTTPS without .git",
			url:       "https://github.com/myorg/myrepo",
			wantOwner: "myorg",
			wantRepo:  "myrepo",
			wantOK:    true,
		},
		{
			name:      "SSH URL format",
			url:       "ssh://github.com/myorg/myrepo.git",
			wantOwner: "myorg",
			wantRepo:  "myrepo",
			wantOK:    true,
		},
		{
			name:      "SSH URL with user",
			url:       "ssh://git@github.com/myorg/myrepo.git",
			wantOwner: "myorg",
			wantRepo:  "myrepo",
			wantOK:    true,
		},
		{
			name:      "SSH URL without .git",
			url:       "ssh://github.com/myorg/myrepo",
			wantOwner: "myorg",
			wantRepo:  "myrepo",
			wantOK:    true,
		},
		{
			name:   "not GitHub",
			url:    "https://gitlab.com/myorg/myrepo",
			wantOK: false,
		},
		{
			name:   "SSH not GitHub",
			url:    "git@gitlab.com:myorg/myrepo.git",
			wantOK: false,
		},
		{
			name:   "empty string",
			url:    "",
			wantOK: false,
		},
		{
			name:   "GitHub URL without repo",
			url:    "https://github.com/myorg",
			wantOK: false,
		},
		{
			name:      "SSH with nested path",
			url:       "git@github.com:org/repo/extra.git",
			wantOwner: "org",
			wantRepo:  "repo",
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, ok := ParseGitHubRepo(tt.url)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantOwner, owner)
				assert.Equal(t, tt.wantRepo, repo)
			}
		})
	}
}
