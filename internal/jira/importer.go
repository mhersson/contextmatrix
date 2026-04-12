package jira

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// Importer handles importing Jira epics as CM projects.
type Importer struct {
	client  *Client
	svc     *service.CardService
	store   *storage.FilesystemStore
	jiraCfg config.JiraConfig
}

// NewImporter creates a new Jira epic importer.
func NewImporter(client *Client, svc *service.CardService, store *storage.FilesystemStore, jiraCfg config.JiraConfig) *Importer {
	return &Importer{
		client:  client,
		svc:     svc,
		store:   store,
		jiraCfg: jiraCfg,
	}
}

// ImportEpicInput contains the parameters for importing a Jira epic.
type ImportEpicInput struct {
	EpicKey      string   // required: Jira epic key (e.g. "PROJ-42")
	Name         string   // optional: CM project name (derived from epic summary if empty)
	Prefix       string   // optional: card ID prefix (derived from Jira project key if empty)
	SelectedKeys []string `json:"selected_keys"` // optional: if non-empty, only import issues whose key appears here
}

// ImportEpicResult contains the result of an epic import.
type ImportEpicResult struct {
	Project       *board.ProjectConfig `json:"project"`
	CardsImported int                  `json:"cards_imported"`
	Skipped       int                  `json:"skipped"`
}

// IssuePreview is a lightweight view of a Jira issue for the preview endpoint.
type IssuePreview struct {
	Key             string `json:"key"`
	Summary         string `json:"summary"`
	Status          string `json:"status"`
	IssueType       string `json:"issue_type"`
	Done            bool   `json:"done,omitempty"`            // true if issue is already done in Jira (will be skipped on import)
	AlreadyImported bool   `json:"already_imported,omitempty"` // true if a CM card with this Jira key already exists
}

// EpicPreview contains a Jira epic and its child issues for display in the UI.
type EpicPreview struct {
	Epic     IssuePreview   `json:"epic"`
	Children []IssuePreview `json:"children"`
}

// PreviewEpic fetches a Jira epic and its children without importing.
// For each child issue, AlreadyImported is set to true when a CM card with a
// matching ExternalID already exists (i.e. it was imported in a prior run).
func (imp *Importer) PreviewEpic(ctx context.Context, epicKey string) (*EpicPreview, error) {
	epic, err := imp.client.FetchIssue(ctx, epicKey)
	if err != nil {
		return nil, fmt.Errorf("fetch epic: %w", err)
	}

	children, err := imp.client.FetchEpicChildren(ctx, epicKey)
	if err != nil {
		return nil, fmt.Errorf("fetch children: %w", err)
	}

	// Find the CM project (if any) whose Jira epic key matches this request.
	// This is used to flag child issues that were already imported.
	importedProject := imp.findProjectByEpicKey(ctx, epicKey)

	preview := &EpicPreview{
		Epic:     issueToPreview(epic),
		Children: make([]IssuePreview, 0, len(children)),
	}
	for i := range children {
		child := issueToPreview(&children[i])
		if importedProject != "" {
			child.AlreadyImported = imp.isAlreadyImported(ctx, importedProject, children[i].Key)
		}
		preview.Children = append(preview.Children, child)
	}

	return preview, nil
}

// findProjectByEpicKey returns the name of the CM project whose jira.epic_key
// matches epicKey, or empty string if no such project exists.
func (imp *Importer) findProjectByEpicKey(ctx context.Context, epicKey string) string {
	projects, err := imp.store.ListProjects(ctx)
	if err != nil {
		slog.Warn("jira preview: list projects", "error", err)
		return ""
	}
	for _, p := range projects {
		if p.Jira != nil && p.Jira.EpicKey == epicKey {
			return p.Name
		}
	}
	return ""
}

// isAlreadyImported reports whether a card with the given Jira issue key already
// exists in the specified CM project.
func (imp *Importer) isAlreadyImported(ctx context.Context, projectName, issueKey string) bool {
	cards, err := imp.store.ListCards(ctx, projectName, storage.CardFilter{ExternalID: issueKey})
	if err != nil {
		slog.Warn("jira preview: check existing card",
			"project", projectName, "issue_key", issueKey, "error", err)
		return false
	}
	return len(cards) > 0
}

// ImportEpic imports a Jira epic as a CM project with all child issues as cards.
func (imp *Importer) ImportEpic(ctx context.Context, input ImportEpicInput) (*ImportEpicResult, error) {
	// Fetch the epic itself.
	epic, err := imp.client.FetchIssue(ctx, input.EpicKey)
	if err != nil {
		return nil, fmt.Errorf("fetch epic: %w", err)
	}

	// Fetch all child issues.
	children, err := imp.client.FetchEpicChildren(ctx, input.EpicKey)
	if err != nil {
		return nil, fmt.Errorf("fetch children: %w", err)
	}

	// Derive project name and prefix.
	projectName := input.Name
	if projectName == "" {
		projectName = slugify(epic.Fields.Summary)
	}

	prefix := input.Prefix
	if prefix == "" {
		prefix = extractProjectKey(input.EpicKey)
	}

	// Extract the Jira project key from the epic key.
	jiraProjectKey := extractProjectKey(input.EpicKey)

	// Create the CM project.
	project, err := imp.svc.CreateProject(ctx, service.CreateProjectInput{
		Name:       projectName,
		Prefix:     prefix,
		States:     defaultStates(),
		Types:      defaultTypes(),
		Priorities: defaultPriorities(),
		Transitions: defaultTransitions(),
		Jira: &board.JiraEpicConfig{
			EpicKey:    input.EpicKey,
			ProjectKey: jiraProjectKey,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}

	// Import each child issue as a card.
	imported := 0
	skipped := 0
	resolvedRepo := ""

	for _, child := range children {
		if ctx.Err() != nil {
			return &ImportEpicResult{Project: project, CardsImported: imported, Skipped: skipped}, ctx.Err()
		}

		// Skip issues already done in Jira — no point importing completed work.
		if isDoneStatus(child.Fields.Status.Name) {
			skipped++
			continue
		}

		// If a specific set of keys was requested, skip issues not in that set.
		if len(input.SelectedKeys) > 0 && !containsKey(input.SelectedKeys, child.Key) {
			skipped++
			continue
		}

		externalID := child.Key

		// Dedup: skip if card already exists.
		existing, err := imp.store.ListCards(ctx, projectName, storage.CardFilter{ExternalID: externalID})
		if err != nil {
			slog.Warn("jira import: check existing card",
				"project", projectName, "external_id", externalID, "error", err)
			skipped++
			continue
		}
		if len(existing) > 0 {
			skipped++
			continue
		}

		// Map fields.
		priority := "medium"
		if child.Fields.Priority != nil {
			priority = MapPriority(child.Fields.Priority.Name)
		}

		labels := make([]string, 0, len(child.Fields.Labels)+len(child.Fields.Components))
		labels = append(labels, child.Fields.Labels...)
		for _, comp := range child.Fields.Components {
			labels = append(labels, comp.Name)
		}

		body := ExtractDescription(child.Fields.Description)

		externalURL := fmt.Sprintf("%s/browse/%s", imp.client.baseURL, child.Key)

		cardType := mapIssueType(child.Fields.IssueType.Name)

		// Resolve repo from component mapping.
		repo := imp.resolveRepo(jiraProjectKey, child.Fields.Components)

		_, err = imp.svc.CreateCard(ctx, projectName, service.CreateCardInput{
			ID:       child.Key,
			Title:    fmt.Sprintf("%s %s", child.Key, child.Fields.Summary),
			Type:     cardType,
			Priority: priority,
			Labels:   labels,
			Body:     body,
			Vetted:   true, // Human-initiated import — considered vetted.
			Source: &board.Source{
				System:      "jira",
				ExternalID:  externalID,
				ExternalURL: externalURL,
			},
		})
		if err != nil {
			slog.Warn("jira import: create card",
				"project", projectName, "issue", externalID, "error", err)
			skipped++
			continue
		}

		// Track the first resolved repo from component mapping.
		if resolvedRepo == "" && repo != "" {
			resolvedRepo = repo
		}

		imported++
	}

	// Persist the resolved repo to the project config.
	if resolvedRepo != "" {
		project.Repo = resolvedRepo
		_, err := imp.svc.UpdateProject(ctx, projectName, service.UpdateProjectInput{
			Repo:        resolvedRepo,
			States:      project.States,
			Types:       project.Types,
			Priorities:  project.Priorities,
			Transitions: project.Transitions,
			Jira:        project.Jira,
		})
		if err != nil {
			slog.Warn("jira import: persist resolved repo",
				"project", projectName, "repo", resolvedRepo, "error", err)
		}
	}

	return &ImportEpicResult{
		Project:       project,
		CardsImported: imported,
		Skipped:       skipped,
	}, nil
}

// resolveRepo finds the repo URL for an issue based on its components and the global config.
func (imp *Importer) resolveRepo(jiraProjectKey string, components []NameField) string {
	mapping, ok := imp.jiraCfg.Projects[jiraProjectKey]
	if !ok {
		return ""
	}

	for _, comp := range components {
		for _, rm := range mapping.RepoMappings {
			if strings.EqualFold(rm.Component, comp.Name) {
				return rm.Repo
			}
		}
	}

	return mapping.DefaultRepo
}

// issueToPreview converts a full Issue to a lightweight preview.
func issueToPreview(issue *Issue) IssuePreview {
	return IssuePreview{
		Key:       issue.Key,
		Summary:   issue.Fields.Summary,
		Status:    issue.Fields.Status.Name,
		IssueType: issue.Fields.IssueType.Name,
		Done:      isDoneStatus(issue.Fields.Status.Name),
	}
}

// isDoneStatus returns true if a Jira status indicates the issue is already completed.
func isDoneStatus(status string) bool {
	switch strings.ToLower(status) {
	case "done", "closed", "resolved", "completed":
		return true
	default:
		return false
	}
}

// mapIssueType converts a Jira issue type name to a CM card type.
func mapIssueType(jiraType string) string {
	switch strings.ToLower(jiraType) {
	case "bug":
		return "bug"
	case "story", "task", "sub-task":
		return "task"
	case "improvement", "new feature":
		return "feature"
	default:
		return "task"
	}
}

// slugify converts a string to a valid CM project name.
// Lowercases, replaces non-alphanumeric with hyphens, trims, deduplicates hyphens.
var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "jira-import"
	}
	// Truncate to a reasonable length.
	if len(s) > 50 {
		s = s[:50]
		s = strings.TrimRight(s, "-")
	}
	return s
}

// extractProjectKey extracts the Jira project key from an issue key (e.g. "PROJ-42" → "PROJ").
func extractProjectKey(issueKey string) string {
	parts := strings.SplitN(issueKey, "-", 2)
	if len(parts) > 0 {
		return strings.ToUpper(parts[0])
	}
	return issueKey
}

// defaultStates returns the standard set of CM board states.
func defaultStates() []string {
	return []string{"todo", "in_progress", "review", "done", "stalled", "not_planned"}
}

// defaultTypes returns the standard set of CM card types.
func defaultTypes() []string {
	return []string{"task", "bug", "feature"}
}

// defaultPriorities returns the standard set of CM card priorities.
func defaultPriorities() []string {
	return []string{"critical", "high", "medium", "low"}
}

// containsKey reports whether key appears in the slice.
func containsKey(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

// defaultTransitions returns the standard CM state transition map.
func defaultTransitions() map[string][]string {
	return map[string][]string{
		"todo":        {"in_progress", "not_planned"},
		"in_progress": {"review", "todo", "stalled", "not_planned"},
		"review":      {"done", "in_progress", "not_planned"},
		"done":        {"todo"},
		"stalled":     {"todo", "in_progress"},
		"not_planned": {"todo"},
	}
}
