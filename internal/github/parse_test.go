package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseGitHubRepo(t *testing.T) {
	defaultHosts := []string{"github.com"}
	enterpriseHosts := []string{"github.com", "acme.ghe.com"}

	tests := []struct {
		name         string
		url          string
		allowedHosts []string
		wantOwner    string
		wantRepo     string
		wantHost     string
		wantOK       bool
	}{
		{
			name:         "SSH format github.com",
			url:          "git@github.com:myorg/myrepo.git",
			allowedHosts: defaultHosts,
			wantOwner:    "myorg",
			wantRepo:     "myrepo",
			wantHost:     "github.com",
			wantOK:       true,
		},
		{
			name:         "HTTPS with .git",
			url:          "https://github.com/myorg/myrepo.git",
			allowedHosts: defaultHosts,
			wantOwner:    "myorg",
			wantRepo:     "myrepo",
			wantHost:     "github.com",
			wantOK:       true,
		},
		{
			name:         "HTTPS without .git",
			url:          "https://github.com/myorg/myrepo",
			allowedHosts: defaultHosts,
			wantOwner:    "myorg",
			wantRepo:     "myrepo",
			wantHost:     "github.com",
			wantOK:       true,
		},
		{
			name:         "SSH URL format",
			url:          "ssh://github.com/myorg/myrepo.git",
			allowedHosts: defaultHosts,
			wantOwner:    "myorg",
			wantRepo:     "myrepo",
			wantHost:     "github.com",
			wantOK:       true,
		},
		{
			name:         "SSH URL with user",
			url:          "ssh://git@github.com/myorg/myrepo.git",
			allowedHosts: defaultHosts,
			wantOwner:    "myorg",
			wantRepo:     "myrepo",
			wantHost:     "github.com",
			wantOK:       true,
		},
		{
			name:         "SSH URL without .git",
			url:          "ssh://github.com/myorg/myrepo",
			allowedHosts: defaultHosts,
			wantOwner:    "myorg",
			wantRepo:     "myrepo",
			wantHost:     "github.com",
			wantOK:       true,
		},
		{
			name:         "enterprise HTTPS accepted when in allowlist",
			url:          "https://acme.ghe.com/owner/repo.git",
			allowedHosts: enterpriseHosts,
			wantOwner:    "owner",
			wantRepo:     "repo",
			wantHost:     "acme.ghe.com",
			wantOK:       true,
		},
		{
			name:         "enterprise SSH accepted when in allowlist",
			url:          "git@acme.ghe.com:owner/repo.git",
			allowedHosts: enterpriseHosts,
			wantOwner:    "owner",
			wantRepo:     "repo",
			wantHost:     "acme.ghe.com",
			wantOK:       true,
		},
		{
			name:         "enterprise HTTPS rejected when not in allowlist",
			url:          "https://acme.ghe.com/owner/repo.git",
			allowedHosts: defaultHosts,
			wantOK:       false,
		},
		{
			name:         "enterprise SSH rejected when not in allowlist",
			url:          "git@acme.ghe.com:owner/repo.git",
			allowedHosts: defaultHosts,
			wantOK:       false,
		},
		{
			name:         "not GitHub",
			url:          "https://gitlab.com/myorg/myrepo",
			allowedHosts: defaultHosts,
			wantOK:       false,
		},
		{
			name:         "SSH not GitHub",
			url:          "git@gitlab.com:myorg/myrepo.git",
			allowedHosts: defaultHosts,
			wantOK:       false,
		},
		{
			name:         "empty string",
			url:          "",
			allowedHosts: defaultHosts,
			wantOK:       false,
		},
		{
			name:         "GitHub URL without repo",
			url:          "https://github.com/myorg",
			allowedHosts: defaultHosts,
			wantOK:       false,
		},
		{
			name:         "SSH with nested path",
			url:          "git@github.com:org/repo/extra.git",
			allowedHosts: defaultHosts,
			wantOwner:    "org",
			wantRepo:     "repo",
			wantHost:     "github.com",
			wantOK:       true,
		},
		{
			name:         "empty allowlist rejects all",
			url:          "https://github.com/myorg/myrepo",
			allowedHosts: []string{},
			wantOK:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, host, ok := ParseGitHubRepo(tt.url, tt.allowedHosts)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantOwner, owner)
				assert.Equal(t, tt.wantRepo, repo)
				assert.Equal(t, tt.wantHost, host)
			}
		})
	}
}
