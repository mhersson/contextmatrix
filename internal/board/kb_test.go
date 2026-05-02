package board

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProjectKBMerging(t *testing.T) {
	kb := ProjectKB{
		Repos: map[string]string{
			"auth-svc":    "# auth-svc\nUser auth.",
			"billing-svc": "# billing-svc\nBilling.",
		},
		JiraProject: "# PAY\nPayments program context.",
		Project:     "# pay-q3-epic\nQ3 epic notes.",
	}
	md := kb.RenderMarkdown()
	require.Contains(t, md, "## Repository: auth-svc")
	require.Contains(t, md, "User auth.")
	require.Contains(t, md, "## Repository: billing-svc")
	require.Contains(t, md, "## Jira project")
	require.Contains(t, md, "Payments program context.")
	require.Contains(t, md, "## Project")
	require.Contains(t, md, "Q3 epic notes.")

	// Inter-tier ordering: repos must precede jira project, which must
	// precede project. A regression that swapped these blocks would slip
	// past the Contains checks above.
	repoIdx := strings.Index(md, "## Repository:")
	jiraIdx := strings.Index(md, "## Jira project")
	projectIdx := strings.Index(md, "## Project")
	require.True(t, repoIdx >= 0 && jiraIdx >= 0 && projectIdx >= 0)
	require.Less(t, repoIdx, jiraIdx, "repos must come before jira project")
	require.Less(t, jiraIdx, projectIdx, "jira project must come before project")
}

func TestProjectKBEmpty(t *testing.T) {
	kb := ProjectKB{}
	require.True(t, kb.IsEmpty())
	require.Empty(t, kb.RenderMarkdown())
}

func TestProjectKBDeterministicRepoOrdering(t *testing.T) {
	// Repos rendered in lexicographic slug order to be deterministic for tests/caching.
	kb := ProjectKB{
		Repos: map[string]string{
			"zeta":  "# zeta",
			"alpha": "# alpha",
			"mu":    "# mu",
		},
	}
	md := kb.RenderMarkdown()
	alphaIdx := stringIndex(md, "## Repository: alpha")
	muIdx := stringIndex(md, "## Repository: mu")
	zetaIdx := stringIndex(md, "## Repository: zeta")
	require.True(t, alphaIdx >= 0 && muIdx >= 0 && zetaIdx >= 0, "all sections present")
	require.Less(t, alphaIdx, muIdx, "alpha before mu")
	require.Less(t, muIdx, zetaIdx, "mu before zeta")
}

// stringIndex is a small test helper.
func stringIndex(s, substr string) int {
	return strings.Index(s, substr)
}
