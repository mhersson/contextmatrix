package jiraimport

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/jira"
	"github.com/mhersson/contextmatrix/internal/service"
)

// smartCommitHash matches a run of # before a letter or digit. The card title
// becomes the PR title and the merge commit message, where GitHub for Jira
// runs "#word" as a smart-commit command; the whole run goes, so "##close"
// cannot leave "#close" behind.
var smartCommitHash = regexp.MustCompile(`#+([\p{L}\p{N}])`)

func cardTitle(issue jira.Issue) string {
	return smartCommitHash.ReplaceAllString(issue.Key+" "+strings.TrimSpace(issue.Summary), "$1")
}

// cardType maps a Jira issue type to one of the project's types. It never
// returns the built-in subtask type: Jira children become top-level cards.
func cardType(it jira.IssueType, types []string) string {
	want := "task"

	switch strings.ToLower(strings.TrimSpace(it.Name)) {
	case "bug":
		want = "bug"
	case "improvement", "new feature", "feature":
		want = "feature"
	}

	if it.Subtask || it.HierarchyLevel < 0 {
		want = "task"
	}

	if want == "feature" && !slices.Contains(types, "feature") {
		want = "task"
	}

	if slices.Contains(types, want) {
		return want
	}

	for _, t := range types {
		if t != board.SubtaskType {
			return t
		}
	}

	return "task"
}

var priorityMap = map[string]string{
	"highest":  "critical",
	"critical": "critical",
	"blocker":  "critical",
	"high":     "high",
	"medium":   "medium",
	"normal":   "medium",
	"low":      "low",
	"lowest":   "low",
	"trivial":  "low",
	"minor":    "low",
}

var priorityFallbacks = map[string][]string{
	"critical": {"critical", "high", "medium"},
	"high":     {"high", "medium"},
	"medium":   {"medium"},
	"low":      {"low", "medium"},
}

func cardPriority(jiraPriority string, priorities []string) string {
	mapped := "medium"
	if p, ok := priorityMap[strings.ToLower(strings.TrimSpace(jiraPriority))]; ok {
		mapped = p
	}

	for _, candidate := range priorityFallbacks[mapped] {
		if slices.Contains(priorities, candidate) {
			return candidate
		}
	}

	if len(priorities) > 0 {
		return priorities[0]
	}

	return mapped
}

// epicSection is the epic description appended to each card body, or "" when
// the epic has none. It sits in the body, so card vetting covers it.
func epicSection(epic jira.Issue, browseURL string) string {
	md := strings.TrimSpace(jira.ADFToMarkdown(epic.Description))
	if md == "" {
		return ""
	}

	return fmt.Sprintf("## Epic context ([%s](%s))\n\n%s", epic.Key, browseURL, md)
}

func cardBody(issue jira.Issue, epicSection string) string {
	body := strings.TrimSpace(jira.ADFToMarkdown(issue.Description))

	switch {
	case epicSection == "":
		return body
	case body == "":
		return epicSection
	default:
		return body + "\n\n" + epicSection
	}
}

// importHash is the jira_import_hash contract: hex SHA-256 of the imported
// title, a newline, and the imported body.
func importHash(title, body string) string {
	sum := sha256.Sum256([]byte(title + "\n" + body))

	return hex.EncodeToString(sum[:])
}

// cardInput builds the create input for one issue. Parent stays empty: a Jira
// child is never a CM subtask.
func (s *Service) cardInput(
	issue jira.Issue, cfg *board.ProjectConfig, epicSection string, vetted bool, agentID string,
) service.CreateCardInput {
	title := cardTitle(issue)
	body := cardBody(issue, epicSection)

	msg := "Imported from Jira " + issue.Key
	if vetted {
		msg += ", marked vetted on import"
	}

	return service.CreateCardInput{
		Title:    title,
		Body:     body,
		Type:     cardType(issue.IssueType, cfg.Types),
		Priority: cardPriority(issue.Priority, cfg.Priorities),
		Source:   s.source(issue.Key),
		Vetted:   vetted,
		Custom: map[string]any{
			jira.CustomIssueID:    issue.ID,
			jira.CustomImportHash: importHash(title, body),
		},
		Log: &board.ActivityEntry{Agent: agentID, Action: "imported", Message: msg},
	}
}
