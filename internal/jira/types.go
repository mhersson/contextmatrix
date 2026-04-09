package jira

// Issue represents a Jira issue (subset of fields used by ContextMatrix).
type Issue struct {
	Key    string      `json:"key"`
	Self   string      `json:"self"`
	Fields IssueFields `json:"fields"`
}

// IssueFields contains the fields of a Jira issue.
type IssueFields struct {
	Summary     string      `json:"summary"`
	Description any         `json:"description"` // string (Server) or ADF JSON (Cloud)
	IssueType   NameField   `json:"issuetype"`
	Priority    *NameField  `json:"priority"`
	Status      NameField   `json:"status"`
	Labels      []string    `json:"labels"`
	Components  []NameField `json:"components"`
}

// NameField is a Jira object that has a name field (issue type, priority, status, component).
type NameField struct {
	Name string `json:"name"`
}

// searchResult is the paginated response from Jira's /rest/api/3/search/jql endpoint.
type searchResult struct {
	StartAt    int     `json:"startAt"`
	MaxResults int     `json:"maxResults"`
	Total      int     `json:"total"`
	Issues     []Issue `json:"issues"`
}
