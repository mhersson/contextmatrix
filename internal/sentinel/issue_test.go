package sentinel

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

func decode(t *testing.T, link string) (title, body string) {
	t.Helper()

	u, err := url.Parse(link)
	require.NoError(t, err)

	return u.Query().Get("title"), u.Query().Get("body")
}

func TestIssueDraft(t *testing.T) {
	ic := issueContext{
		terms:    []term{{"git.acme.internal", wordHost}, {"shop", wordProject}},
		prefixes: []string{"SHOP"},
		version:  "v1.2.3",
	}

	t.Run("redacts and composes", func(t *testing.T) {
		link, blocked := issueDraft(state.Finding{
			Title:    "Verify hangs on SHOP-12",
			Text:     "the shop verify step never returns",
			Evidence: "seen in a run on git.acme.internal",
		}, ic)

		assert.Empty(t, blocked)
		assert.True(t, strings.HasPrefix(link, "https://github.com/mhersson/contextmatrix/issues/new?"))

		title, body := decode(t, link)
		assert.Equal(t, "Verify hangs on a card", title)
		assert.Equal(t, "ContextMatrix v1.2.3\n\nthe a project verify step never returns\n\n## Evidence\n\nseen in a run on a host", body)
	})

	t.Run("no evidence and no version", func(t *testing.T) {
		link, blocked := issueDraft(state.Finding{Title: "t", Text: "just text"}, issueContext{})
		assert.Empty(t, blocked)

		_, body := decode(t, link)
		assert.Equal(t, "just text", body)
	})

	t.Run("a trimmed draft keeps the version line", func(t *testing.T) {
		link, blocked := issueDraft(state.Finding{Title: "t", Text: strings.Repeat("word ", 4000)}, ic)
		assert.Empty(t, blocked)

		_, body := decode(t, link)
		assert.True(t, strings.HasPrefix(body, "ContextMatrix v1.2.3\n\n"))
		assert.True(t, strings.HasSuffix(body, trimmedLine))
	})

	t.Run("no title uses the first line", func(t *testing.T) {
		link, _ := issueDraft(state.Finding{Text: "first line\nsecond"}, ic)
		title, _ := decode(t, link)
		assert.Equal(t, "first line", title)
	})

	t.Run("a term straddling the title cut is redacted before the cut", func(t *testing.T) {
		text := strings.Repeat("x ", 95) + "git.acme.internal stalls\nmore"
		link, blocked := issueDraft(state.Finding{Text: text}, ic)

		title, body := decode(t, link)
		assert.Empty(t, blocked)
		assert.NotContains(t, title, "acme")
		assert.NotContains(t, body, "acme")
		assert.NotContains(t, title, "git.a")
	})

	t.Run("users are redacted everywhere", func(t *testing.T) {
		link, blocked := issueDraft(state.Finding{
			Title: "human:alice stuck", Text: "human:alice waits", Evidence: "by human:bob",
		}, ic)
		assert.Empty(t, blocked)

		title, body := decode(t, link)
		assert.Equal(t, "a user stuck", title)
		assert.NotContains(t, body, "human:")
		assert.Contains(t, body, "a user waits")
		assert.Contains(t, body, "by a user")
	})
}

func TestIssueDraftBlocksAHitTheReplacementMade(t *testing.T) {
	link, blocked := issueDraft(state.Finding{Title: "t", Text: "acme web"}, issueContext{
		terms: []term{{"repository web", wordProject}, {"acme", wordRepository}},
	})

	assert.Empty(t, link)
	assert.Equal(t, "still names a project", blocked)
}

func TestFitIssue(t *testing.T) {
	t.Run("a short body is unchanged", func(t *testing.T) {
		link, fitted := fitIssue("t", "short")
		assert.Equal(t, "short", fitted)
		assert.Equal(t, newIssueURL("t", "short"), link)
	})

	t.Run("a long body is cut at whitespace", func(t *testing.T) {
		link, fitted := fitIssue("t", strings.Repeat("word ", 4000))
		assert.LessOrEqual(t, len(link), maxIssueURLBytes)
		assert.True(t, strings.HasSuffix(fitted, "word\n\n"+trimmedLine))

		_, body := decode(t, link)
		assert.Equal(t, fitted, body)
	})

	t.Run("multi-byte text fits", func(t *testing.T) {
		link, _ := fitIssue("t", strings.Repeat("日本語 ", 2000))
		assert.LessOrEqual(t, len(link), maxIssueURLBytes)
	})

	t.Run("never ends on a fragment", func(t *testing.T) {
		_, fitted := fitIssue("t", strings.Repeat("abcdefghij ", 2000))
		kept := strings.TrimSuffix(fitted, "\n\n"+trimmedLine)
		fields := strings.Fields(kept)
		assert.Equal(t, "abcdefghij", fields[len(fields)-1])
	})

	t.Run("no whitespace leaves the trim line alone", func(t *testing.T) {
		link, fitted := fitIssue("t", strings.Repeat("x", 20000))
		assert.Equal(t, trimmedLine, fitted)
		assert.LessOrEqual(t, len(link), maxIssueURLBytes)
	})
}
