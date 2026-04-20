package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSanitizeErrorDetails verifies that downstream error strings with
// leakable content are replaced by stable class labels, while author-written
// short messages pass through unchanged.
func TestSanitizeErrorDetails(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty input returns empty",
			in:   "",
			want: "",
		},
		{
			name: "go-git transport auth failure collapses to class label",
			in:   "clone: transport: authentication required",
			want: "git remote unreachable",
		},
		{
			name: "go-git transport prefix at start collapses to class label",
			in:   "transport.ErrEmptyRemoteRepository",
			want: "git remote unreachable",
		},
		{
			name: "ssh handshake failure collapses to class label",
			in:   "push: ssh: handshake failed: host key mismatch",
			want: "git remote unreachable",
		},
		{
			name: "exec missing binary collapses to class label",
			in:   "run git: exec: \"git\": executable file not found in $PATH",
			want: "git operation failed",
		},
		{
			name: "boards-dir .git path leak is redacted",
			in:   "open /var/lib/contextmatrix/boards/.git/refs/heads/main: no such file or directory",
			want: "git operation failed",
		},
		{
			name: "generic absolute path leak is redacted",
			in:   "open /home/alice/boards/project/tasks/CARD-001.md: permission denied",
			want: "filesystem error",
		},
		{
			name: "short author-written message passes through unchanged",
			in:   "card already claimed by agent-7",
			want: "card already claimed by agent-7",
		},
		{
			name: "validation-style message passes through unchanged",
			in:   `parent card "TEST-042" does not exist`,
			want: `parent card "TEST-042" does not exist`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeErrorDetails(tc.in)
			assert.Equal(t, tc.want, got)
		})
	}
}
