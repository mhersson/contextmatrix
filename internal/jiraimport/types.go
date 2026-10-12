package jiraimport

// Preview is the epic's two-level tree as the import dialog shows it.
type Preview struct {
	Epic     Epic         `json:"epic"`
	Children []Node       `json:"children"`
	Playbook *PlaybookRef `json:"playbook,omitempty"` // the playbook an import would extend
}

// Epic is the epic itself; Description is its text as markdown.
type Epic struct {
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// Node is one child issue of the epic, or one sub-task of a child.
type Node struct {
	Key            string       `json:"key"`
	ID             string       `json:"id"`
	Summary        string       `json:"summary"`
	URL            string       `json:"url"`
	IssueType      string       `json:"issue_type"`
	Status         string       `json:"status"`
	StatusCategory string       `json:"status_category"`
	JiraProject    string       `json:"jira_project"`
	Components     []string     `json:"components,omitempty"`
	DefaultProject string       `json:"default_project,omitempty"`
	Imported       *ImportedRef `json:"imported,omitempty"`
	Subtasks       []Node       `json:"subtasks,omitempty"`
}

// ImportedRef names the card an earlier import created for the issue.
type ImportedRef struct {
	Project    string `json:"project"`
	Card       string `json:"card"`
	InPlaybook bool   `json:"in_playbook"`
}

// PlaybookRef names a playbook.
type PlaybookRef struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	BoardsRepo string `json:"boards_repo,omitempty"`
}

// ImportRequest is the POST body. The server re-reads every issue from Jira;
// the browser supplies only keys, target projects and options.
type ImportRequest struct {
	Selections         []Selection     `json:"selections"`
	Vetted             bool            `json:"vetted"`
	IncludeEpicContext bool            `json:"include_epic_context"`
	Playbook           PlaybookRequest `json:"playbook"`
}

// Selection is one issue to import and the CM project it lands in.
type Selection struct {
	Key     string `json:"key"`
	Project string `json:"project"`
}

// PlaybookRequest is used only when no playbook for the epic exists yet.
type PlaybookRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	BoardsRepo  string `json:"boards_repo,omitempty"`
}

// ImportResponse is the playbook the import used and one result per selection.
type ImportResponse struct {
	Playbook PlaybookRef `json:"playbook"`
	Results  []Result    `json:"results"`
}

// Result is the outcome for one selection.
type Result struct {
	Key     string `json:"key"`
	Status  string `json:"status"`
	Project string `json:"project,omitempty"`
	Card    string `json:"card,omitempty"`
	Reason  string `json:"reason,omitempty"`
}
