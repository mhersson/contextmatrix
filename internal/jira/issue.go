package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	searchPageSize = 100
	maxSearchPages = 100
)

// issueFields is requested on every issue read; search/jql returns only ids
// without an explicit list.
var issueFields = []string{"summary", "issuetype", "status", "priority", "project", "components", "parent", "description"}

// Account is the identity the configured credentials act as.
type Account struct {
	AccountID, DisplayName, Email string
}

// IssueType is an issue's type and its hierarchy level (1 epic, 0 standard,
// -1 sub-task); sites can rename levels, so callers test the level, not the name.
type IssueType struct {
	Name           string
	HierarchyLevel int
	Subtask        bool
}

// Status is a workflow status with its category key: new, indeterminate or done.
type Status struct {
	Name        string
	CategoryKey string
}

// Issue is the subset of a Jira issue CM reads.
type Issue struct {
	ID, Key, Summary string
	IssueType        IssueType
	Status           Status
	Priority         string
	ProjectKey       string
	Components       []string
	ParentKey        string
	Description      json.RawMessage
}

// TransitionField is one field on a transition screen.
type TransitionField struct {
	Required bool
}

// Transition is a move available from the issue's current status.
type Transition struct {
	ID, Name string
	To       Status
	Fields   map[string]TransitionField
}

type wireStatus struct {
	Name           string `json:"name"`
	StatusCategory struct {
		Key string `json:"key"`
	} `json:"statusCategory"`
}

func (w wireStatus) status() Status {
	return Status{Name: w.Name, CategoryKey: w.StatusCategory.Key}
}

type wireIssue struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Fields struct {
		Summary   string `json:"summary"`
		IssueType struct {
			Name           string `json:"name"`
			HierarchyLevel int    `json:"hierarchyLevel"`
			Subtask        bool   `json:"subtask"`
		} `json:"issuetype"`
		Status   wireStatus `json:"status"`
		Priority *struct {
			Name string `json:"name"`
		} `json:"priority"`
		Project struct {
			Key string `json:"key"`
		} `json:"project"`
		Components []struct {
			Name string `json:"name"`
		} `json:"components"`
		Parent *struct {
			Key string `json:"key"`
		} `json:"parent"`
		Description json.RawMessage `json:"description"`
	} `json:"fields"`
}

func (w wireIssue) issue() Issue {
	f := w.Fields

	is := Issue{
		ID:      w.ID,
		Key:     w.Key,
		Summary: f.Summary,
		IssueType: IssueType{
			Name:           f.IssueType.Name,
			HierarchyLevel: f.IssueType.HierarchyLevel,
			Subtask:        f.IssueType.Subtask,
		},
		Status:     f.Status.status(),
		ProjectKey: f.Project.Key,
	}

	if is.ProjectKey == "" {
		is.ProjectKey, _, _ = strings.Cut(w.Key, "-")
	}

	if f.Priority != nil {
		is.Priority = f.Priority.Name
	}

	if f.Parent != nil {
		is.ParentKey = f.Parent.Key
	}

	for _, comp := range f.Components {
		is.Components = append(is.Components, comp.Name)
	}

	if d := bytes.TrimSpace(f.Description); len(d) > 0 && !bytes.Equal(d, []byte("null")) {
		is.Description = d
	}

	return is
}

// Myself returns the account the credentials act as.
func (c *Client) Myself(ctx context.Context) (Account, error) {
	var w struct {
		AccountID    string `json:"accountId"`
		DisplayName  string `json:"displayName"`
		EmailAddress string `json:"emailAddress"`
	}

	if err := c.do(ctx, http.MethodGet, "/rest/api/3/myself", nil, nil, &w); err != nil {
		return Account{}, fmt.Errorf("myself: %w", err)
	}

	return Account{AccountID: w.AccountID, DisplayName: w.DisplayName, Email: w.EmailAddress}, nil
}

// Issue fetches one issue by key.
func (c *Client) Issue(ctx context.Context, key string) (Issue, error) {
	if !IsIssueKey(key) {
		return Issue{}, fmt.Errorf("issue %q: %w", key, ErrInvalidRef)
	}

	var w wireIssue

	q := url.Values{"fields": {strings.Join(issueFields, ",")}}
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/issue/"+key, q, nil, &w); err != nil {
		return Issue{}, fmt.Errorf("issue %s: %w", key, err)
	}

	return w.issue(), nil
}

// Search runs a bounded JQL query and follows nextPageToken to the last page.
func (c *Client) Search(ctx context.Context, jql string) ([]Issue, error) {
	type request struct {
		JQL           string   `json:"jql"`
		Fields        []string `json:"fields"`
		MaxResults    int      `json:"maxResults"`
		NextPageToken string   `json:"nextPageToken,omitempty"`
	}

	var (
		out   []Issue
		token string
	)

	seen := map[string]bool{}

	for range maxSearchPages {
		var resp struct {
			Issues        []wireIssue `json:"issues"`
			NextPageToken string      `json:"nextPageToken"`
			IsLast        bool        `json:"isLast"`
		}

		req := request{JQL: jql, Fields: issueFields, MaxResults: searchPageSize, NextPageToken: token}
		if err := c.do(ctx, http.MethodPost, "/rest/api/3/search/jql", nil, req, &resp); err != nil {
			return nil, fmt.Errorf("search: %w", err)
		}

		for _, w := range resp.Issues {
			out = append(out, w.issue())
		}

		if resp.IsLast || resp.NextPageToken == "" {
			return out, nil
		}

		if seen[resp.NextPageToken] {
			return nil, errors.New("search: page token repeated")
		}

		seen[resp.NextPageToken] = true
		token = resp.NextPageToken
	}

	return nil, fmt.Errorf("search: more than %d pages", maxSearchPages)
}

// Transitions lists the transitions available from the issue's current
// status, with their screen fields.
func (c *Client) Transitions(ctx context.Context, key string) ([]Transition, error) {
	if !IsIssueKey(key) {
		return nil, fmt.Errorf("transitions %q: %w", key, ErrInvalidRef)
	}

	var w struct {
		Transitions []struct {
			ID     string     `json:"id"`
			Name   string     `json:"name"`
			To     wireStatus `json:"to"`
			Fields map[string]struct {
				Required bool `json:"required"`
			} `json:"fields"`
		} `json:"transitions"`
	}

	q := url.Values{"expand": {"transitions.fields"}}
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/issue/"+key+"/transitions", q, nil, &w); err != nil {
		return nil, fmt.Errorf("transitions %s: %w", key, err)
	}

	out := make([]Transition, 0, len(w.Transitions))

	for _, t := range w.Transitions {
		tr := Transition{ID: t.ID, Name: t.Name, To: t.To.status(), Fields: make(map[string]TransitionField, len(t.Fields))}

		for name, f := range t.Fields {
			tr.Fields[name] = TransitionField{Required: f.Required}
		}

		out = append(out, tr)
	}

	return out, nil
}

// Transition moves an issue through transitionID, setting fields on the
// transition screen (nil for none).
func (c *Client) Transition(ctx context.Context, key, transitionID string, fields map[string]any) error {
	if !IsIssueKey(key) {
		return fmt.Errorf("transition %q: %w", key, ErrInvalidRef)
	}

	body := map[string]any{"transition": map[string]string{"id": transitionID}}
	if len(fields) > 0 {
		body["fields"] = fields
	}

	if err := c.do(ctx, http.MethodPost, "/rest/api/3/issue/"+key+"/transitions", nil, body, nil); err != nil {
		return fmt.Errorf("transition %s: %w", key, err)
	}

	return nil
}
