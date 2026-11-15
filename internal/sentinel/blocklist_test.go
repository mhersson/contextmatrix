package sentinel

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/board"
)

func hasTerm(terms []term, text, word string) bool {
	for _, t := range terms {
		if strings.EqualFold(t.text, text) && t.word == word {
			return true
		}
	}

	return false
}

func hasText(terms []term, text string) bool {
	for _, t := range terms {
		if strings.EqualFold(t.text, text) {
			return true
		}
	}

	return false
}

func TestIssueTermsBoardProject(t *testing.T) {
	terms := issueTerms(termSources{Projects: []board.ProjectConfig{{
		Name: "shop", DisplayName: "Web Shop", Prefix: "SHOP",
		Repo:   "https://git.acme.internal/acme/shop-api.git",
		GitHub: &board.GitHubImportConfig{Owner: "acme-corp", Repo: "tracker"},
	}}})

	for _, text := range []string{"shop", "Web Shop"} {
		assert.True(t, hasTerm(terms, text, wordProject), text)
	}

	for _, text := range []string{
		"https://git.acme.internal/acme/shop-api.git", "acme", "shop-api", "acme-corp", "tracker",
	} {
		assert.True(t, hasTerm(terms, text, wordRepository), text)
	}

	assert.True(t, hasTerm(terms, "git.acme.internal", wordHost))

	count := 0

	for _, tm := range terms {
		if strings.EqualFold(tm.text, "shop") {
			count++
		}
	}

	assert.Equal(t, 1, count, "SHOP dedupes with shop")
}

func TestIssueTermsOperatorBoard(t *testing.T) {
	terms := issueTerms(termSources{
		Projects: []board.ProjectConfig{{
			Name: "contextmatrix", Prefix: "CTXMAX", Repo: "git@github.com:mhersson/contextmatrix.git",
		}},
		RepoURLs: []string{"https://github.com/mhersson/contextmatrix-boards.git"},
	})

	assert.True(t, hasTerm(terms, "CTXMAX", wordCard))
	assert.True(t, hasTerm(terms, "contextmatrix-boards", wordRepository))
	assert.True(t, hasTerm(terms, "https://github.com/mhersson/contextmatrix-boards.git", wordRepository))

	for _, text := range []string{
		"contextmatrix", "mhersson", "github.com",
		"git@github.com:mhersson/contextmatrix.git", "github.com:mhersson/contextmatrix.git",
	} {
		assert.False(t, hasText(terms, text), text)
	}
}

func TestIssueTermsLocations(t *testing.T) {
	terms := issueTerms(termSources{Locations: []string{
		"https://gw.acme.internal:8443/v1", "acme.ghe.com", "http://10.1.2.3:9090", "https://github.com",
	}})

	for _, text := range []string{
		"https://gw.acme.internal:8443/v1", "http://10.1.2.3:9090", "gw.acme.internal", "acme.ghe.com", "10.1.2.3",
	} {
		assert.True(t, hasTerm(terms, text, wordHost), text)
	}

	assert.False(t, hasText(terms, "v1"))
	assert.False(t, hasText(terms, "https://github.com"))
	assert.Len(t, terms, 5)
}

func TestIssueTermsRemotesAndImages(t *testing.T) {
	tests := []struct {
		name     string
		project  board.ProjectConfig
		want     map[string]string
		wantNone []string
	}{
		{
			"userless scp",
			board.ProjectConfig{Name: "p", Prefix: "P", Repo: "git.acme.internal:acme/shop.git"},
			map[string]string{"acme": wordRepository, "shop": wordRepository, "git.acme.internal": wordHost},
			nil,
		},
		{
			"userless scp on the public host",
			board.ProjectConfig{Name: "p", Prefix: "P", Repo: "github.com:mhersson/contextmatrix-agent.git"},
			nil,
			[]string{"mhersson", "contextmatrix-agent", "github.com"},
		},
		{
			"userless scp owner on the public host",
			board.ProjectConfig{Name: "p", Prefix: "P", Repo: "github.com:acme/shop.git"},
			map[string]string{"acme": wordRepository},
			[]string{"github.com"},
		},
		{
			"private registry with port",
			board.ProjectConfig{Name: "p", Prefix: "P", RemoteExecution: &board.RemoteExecutionConfig{
				WorkerImage: "harbor.acme.internal:5000/team/worker:v1", ChatWorkerImage: "reg.acme.internal/chat:2",
			}},
			map[string]string{"harbor.acme.internal": wordHost, "reg.acme.internal": wordHost},
			nil,
		},
		{
			"images without a registry host",
			board.ProjectConfig{Name: "p", Prefix: "P", RemoteExecution: &board.RemoteExecutionConfig{
				WorkerImage: "mhersson/contextmatrix-agent:latest", ChatWorkerImage: "ubuntu:24.04",
			}},
			nil,
			[]string{"mhersson", "contextmatrix-agent", "latest", "ubuntu", "24.04", "ubuntu:24.04"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			terms := issueTerms(termSources{Projects: []board.ProjectConfig{tc.project}})

			for text, word := range tc.want {
				assert.True(t, hasTerm(terms, text, word), text)
			}

			for _, text := range tc.wantNone {
				assert.False(t, hasText(terms, text), text)
			}
		})
	}
}

func TestURLSecrets(t *testing.T) {
	tests := []struct {
		name string
		src  termSources
		want []string
	}{
		{
			"password",
			termSources{Projects: []board.ProjectConfig{{Name: "p", Prefix: "P", Repo: "https://ci:glpat-abcdefgh1234@git.acme.internal/acme/shop.git"}}},
			[]string{"glpat-abcdefgh1234"},
		},
		{"token as user", termSources{RepoURLs: []string{"https://ghp_abcdefgh1234@github.com/acme/app.git"}}, []string{"ghp_abcdefgh1234"}},
		{"scp user", termSources{RepoURLs: []string{"git@host.example:o/n.git"}}, []string{"git"}},
		{"no userinfo", termSources{RepoURLs: []string{"https://github.com/acme/app.git"}, Locations: []string{"acme.ghe.com"}}, nil},
		{"location", termSources{Locations: []string{"https://u:s3cretvalue@gw.acme.internal/v1"}}, []string{"s3cretvalue"}},
		{"deduped", termSources{RepoURLs: []string{"https://a:same-secret@h.example/o/n", "https://b:same-secret@h.example/o/m"}}, []string{"same-secret"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, urlSecrets(tc.src))
		})
	}
}

func TestIssueTermsDropCredentials(t *testing.T) {
	terms := issueTerms(termSources{Projects: []board.ProjectConfig{{
		Name: "p", Prefix: "P", Repo: "https://x-access-token:tok3n@github.com/acme/app.git?a=tok3n#tok3n",
	}}})

	for _, tm := range terms {
		assert.NotContains(t, tm.text, "tok3n")
	}

	assert.True(t, hasTerm(terms, "https://github.com/acme/app.git", wordRepository))
}

func TestIssueTermsSkipsEmptyAndPunctuation(t *testing.T) {
	terms := issueTerms(termSources{
		Projects:  []board.ProjectConfig{{Name: " ", DisplayName: "--", Prefix: ""}},
		Locations: []string{"", "  "},
		RepoURLs:  []string{""},
	})

	assert.Empty(t, terms)
}

func TestIssueTermsLongestFirst(t *testing.T) {
	terms := issueTerms(termSources{
		Projects:  []board.ProjectConfig{{Name: "ab", Prefix: "ABCDEF", Repo: "https://h.example/org/abcd.git"}},
		Locations: []string{"abc.example"},
	})

	for i := 1; i < len(terms); i++ {
		assert.GreaterOrEqual(t, len(terms[i-1].text), len(terms[i].text))
	}
}

func TestRedactTerms(t *testing.T) {
	web := []term{{"web", wordProject}}
	acme := []term{{"https://git.acme.internal/acme/shop.git", wordRepository}, {"acme", wordRepository}, {"shop", wordRepository}, {"git.acme.internal", wordHost}}

	tests := []struct {
		name  string
		text  string
		terms []term
		want  string
	}{
		{"whole word", "the web UI and webhooks", web, "the a project UI and webhooks"},
		{"repeated", "web web", web, "a project a project"},
		{"case", "WEB server", web, "a project server"},
		{"separators bound", "web-ui and web_ui", web, "a project-ui and a project_ui"},
		{"url first", "cloned https://git.acme.internal/acme/shop.git ok", acme, "cloned a repository ok"},
		{"generic names", "the project card", []term{{"project", wordProject}, {"card", wordCard}}, "the a project a card"},
		{"article name", "a bug", []term{{"a", wordProject}}, "a project bug"},
		{
			"shortest first",
			"Web Shop is down",
			[]term{{"web", wordProject}, {"Web Shop", wordProject}},
			"a project is down",
		},
		{"empty term", "a-b", []term{{"", wordProject}, {"b", wordCard}}, "a-a card"},
		{"embedded", "acmeShop", []term{{"acme", wordRepository}}, "acmeShop"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, redactTerms(tc.text, tc.terms))
		})
	}
}

func TestNamesTerm(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		terms []term
		want  []string
	}{
		{"hit", "the web UI", []term{{"web", wordProject}}, []string{wordProject}},
		{"embedded", "webhooks", []term{{"web", wordProject}}, nil},
		{
			"generic phrase words",
			"a project a card",
			[]term{{"project", wordProject}, {"card", wordCard}, {"a", wordProject}, {"a host", wordHost}},
			nil,
		},
		{
			"order",
			"acme on git.acme.internal",
			[]term{{"git.acme.internal", wordHost}, {"acme", wordRepository}},
			[]string{wordRepository, wordHost},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, namesTerm(tc.text, tc.terms))
		})
	}
}
