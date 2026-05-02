package board

import (
	"sort"
	"strings"
)

// ProjectKB is a tiered knowledge-base view assembled from boards-repo files.
// Layers persist independently of CM-project (epic) lifecycle:
//   - Repos:       _kb/repos/<slug>.md       (long-lived, per-repo)
//   - JiraProject: _kb/jira-projects/<KEY>.md (long-lived, per Jira project)
//   - Project:     <project>/kb/project.md   (ephemeral, scoped to one CM project)
type ProjectKB struct {
	Repos       map[string]string `json:"repos,omitempty"`
	JiraProject string            `json:"jira_project,omitempty"`
	Project     string            `json:"project,omitempty"`
}

// IsEmpty reports whether the KB has no content from any tier.
func (kb ProjectKB) IsEmpty() bool {
	return len(kb.Repos) == 0 && kb.JiraProject == "" && kb.Project == ""
}

// RenderMarkdown returns a single markdown document with all available
// layers. Empty KBs return an empty string. Used by plan phase as
// system-prompt context.
func (kb ProjectKB) RenderMarkdown() string {
	if kb.IsEmpty() {
		return ""
	}

	var b strings.Builder

	if len(kb.Repos) > 0 {
		slugs := make([]string, 0, len(kb.Repos))
		for s := range kb.Repos {
			slugs = append(slugs, s)
		}

		sort.Strings(slugs)

		for _, slug := range slugs {
			b.WriteString("## Repository: " + slug + "\n\n")
			b.WriteString(kb.Repos[slug])
			b.WriteString("\n\n")
		}
	}

	if kb.JiraProject != "" {
		b.WriteString("## Jira project\n\n")
		b.WriteString(kb.JiraProject)
		b.WriteString("\n\n")
	}

	if kb.Project != "" {
		b.WriteString("## Project\n\n")
		b.WriteString(kb.Project)
		b.WriteString("\n\n")
	}

	return b.String()
}
