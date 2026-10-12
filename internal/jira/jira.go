// Package jira is a Jira Cloud REST v3 client: issue reads, JQL search,
// status transitions, and ADF to markdown conversion. Credentials enter only
// through an Authorizer built once at startup.
package jira

import "errors"

// SourceSystem is the board.Source.System value on Jira-imported cards and
// playbooks.
const SourceSystem = "jira"

// Custom-field keys on imported cards.
const (
	// CustomIssueID holds the Jira numeric issue id; it survives a move
	// between Jira projects, which changes the key.
	CustomIssueID = "jira_issue_id"
	// CustomImportHash holds the hex SHA-256 of the imported title + "\n" + body.
	CustomImportHash = "jira_import_hash"
)

// Sentinel errors returned (wrapped) by the client.
var (
	ErrUnauthorized = errors.New("jira: unauthorized")            // 401
	ErrNotFound     = errors.New("jira: not found")               // 404, also no permission
	ErrRateLimited  = errors.New("jira: rate limited")            // 429 after the wait budget
	ErrInvalidRef   = errors.New("jira: invalid issue reference") // not a key or a site issue URL
)
