package jira_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/jira"
)

// TestLiveJira runs against a real Jira Cloud site only when CM_JIRA_LIVE_SITE
// is set. CM_JIRA_LIVE_TRANSITION_TO additionally moves CM_JIRA_LIVE_SCRATCH
// to the named status, so a transition is never made by accident.
func TestLiveJira(t *testing.T) {
	site := os.Getenv("CM_JIRA_LIVE_SITE")
	if site == "" {
		t.Skip("CM_JIRA_LIVE_SITE not set")
	}

	auth, err := jira.NewAuthorizer("basic", os.Getenv("CM_JIRA_LIVE_EMAIL"), os.Getenv("CM_JIRA_LIVE_TOKEN"))
	require.NoError(t, err)

	c := jira.NewClient(site, auth, nil)
	ctx := t.Context()

	acct, err := c.Myself(ctx)
	require.NoError(t, err)
	t.Logf("authenticated as %s", acct.DisplayName)

	if epicKey := os.Getenv("CM_JIRA_LIVE_EPIC"); epicKey != "" {
		epic, err := c.Issue(ctx, epicKey)
		require.NoError(t, err)
		assert.Equal(t, 1, epic.IssueType.HierarchyLevel, "CM_JIRA_LIVE_EPIC must be an epic")

		children, err := c.Search(ctx, fmt.Sprintf("parent = %s ORDER BY rank", epicKey))
		require.NoError(t, err)
		t.Logf("epic %s %q has %d children", epic.Key, epic.Summary, len(children))

		for _, ch := range children {
			md := jira.ADFToMarkdown(ch.Description)
			t.Logf("%s [%s/%s] %s: %d markdown bytes, %d placeholders",
				ch.Key, ch.Status.Name, ch.Status.CategoryKey, ch.Summary, len(md), strings.Count(md, "[unsupported Jira content"))
		}
	}

	scratch, target := os.Getenv("CM_JIRA_LIVE_SCRATCH"), os.Getenv("CM_JIRA_LIVE_TRANSITION_TO")
	if scratch == "" || target == "" {
		return
	}

	trs, err := c.Transitions(ctx, scratch)
	require.NoError(t, err)

	for _, tr := range trs {
		if !strings.EqualFold(tr.To.Name, target) {
			continue
		}

		var fields map[string]any
		if f, ok := tr.Fields["resolution"]; ok && f.Required {
			fields = map[string]any{"resolution": map[string]string{"name": "Done"}}
		}

		require.NoError(t, c.Transition(ctx, scratch, tr.ID, fields))

		after, err := c.Issue(ctx, scratch)
		require.NoError(t, err)
		assert.True(t, strings.EqualFold(after.Status.Name, target))

		return
	}

	t.Fatalf("no transition from the current status of %s to %q", scratch, target)
}
