// Package board contains domain types for the ContextMatrix board system.
package board

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Card represents a task card with YAML frontmatter and markdown body.
type Card struct {
	ID              string         `yaml:"id"              json:"id"`
	Title           string         `yaml:"title"           json:"title"`
	Project         string         `yaml:"project"         json:"project"`
	Type            string         `yaml:"type"            json:"type"`
	State           string         `yaml:"state"           json:"state"`
	Priority        string         `yaml:"priority"        json:"priority"`
	AssignedAgent   string         `yaml:"assigned_agent,omitempty"  json:"assigned_agent,omitempty"`
	LastHeartbeat   *time.Time     `yaml:"last_heartbeat,omitempty" json:"last_heartbeat,omitempty"`
	Parent          string         `yaml:"parent,omitempty"         json:"parent,omitempty"`
	Subtasks        []string       `yaml:"subtasks,omitempty"       json:"subtasks,omitempty"`
	DependsOn       []string       `yaml:"depends_on,omitempty"     json:"depends_on,omitempty"`
	DependenciesMet *bool          `yaml:"-"                        json:"dependencies_met,omitempty"`
	Context         []string       `yaml:"context,omitempty"        json:"context,omitempty"`
	Labels          []string       `yaml:"labels,omitempty"         json:"labels,omitempty"`
	Skills          *[]string      `yaml:"skills,omitempty"         json:"skills,omitempty"`
	Source          *Source        `yaml:"source,omitempty"         json:"source,omitempty"`
	Custom          map[string]any `yaml:"custom,omitempty"          json:"custom,omitempty"`
	Autonomous      bool           `yaml:"autonomous,omitempty"           json:"autonomous"`
	// Model pins (agent backend): explicit per-card OpenRouter slugs overriding
	// the complexity selector. Human-set only — never writable via the MCP
	// agent surface. Empty = selector/default applies.
	ModelOrchestrator string `yaml:"model_orchestrator,omitempty" json:"model_orchestrator,omitempty"`
	ModelCoder        string `yaml:"model_coder,omitempty"        json:"model_coder,omitempty"`
	ModelReviewer     string `yaml:"model_reviewer,omitempty"     json:"model_reviewer,omitempty"`
	// BestOfN, when >= 2, makes agent-backend runs race N candidate
	// implementations and judge a winner. 0/absent = normal run. Human-set
	// only, like the model pins.
	BestOfN int `yaml:"best_of_n,omitempty"          json:"best_of_n,omitempty"`
	// Co-op discussion fields (agent backend). CoopParticipants >= 2 convenes
	// N internal discussion seats in the phases listed in CoopPhases (subset
	// of plan/review/execute; empty = backend default). CoopGuests names
	// operator-registered external participants from the server's coop.guests
	// registry. Human-set only, like the model pins — never writable via the
	// MCP agent surface.
	CoopParticipants int      `yaml:"coop_participants,omitempty" json:"coop_participants,omitempty"`
	CoopPhases       []string `yaml:"coop_phases,omitempty"       json:"coop_phases,omitempty"`
	CoopGuests       []string `yaml:"coop_guests,omitempty"       json:"coop_guests,omitempty"`
	// Verify overrides the project's verify gate for this card, merged
	// field-by-field over it at trigger time (see ResolveVerify). Human-set
	// only, like the model pins — never writable via the MCP agent surface.
	Verify         *VerifyConfig `yaml:"verify,omitempty"             json:"verify,omitempty"`
	Vetted         bool          `yaml:"vetted,omitempty"             json:"vetted"`
	FeatureBranch  bool          `yaml:"feature_branch,omitempty"     json:"feature_branch,omitempty"`
	CreatePR       bool          `yaml:"create_pr,omitempty"          json:"create_pr,omitempty"`
	BranchName     string        `yaml:"branch_name,omitempty"        json:"branch_name,omitempty"`
	BaseBranch     string        `yaml:"base_branch,omitempty"        json:"base_branch,omitempty"`
	PRUrl          string        `yaml:"pr_url,omitempty"             json:"pr_url,omitempty"`
	ReviewAttempts int           `yaml:"review_attempts,omitempty"    json:"review_attempts,omitempty"`
	WorkerStatus   string        `yaml:"worker_status,omitempty"      json:"worker_status,omitempty"`
	// Phase is the autonomous orchestrator's position within the run
	// (plan|execute|judge|document|review|integrate|done). Orthogonal to State: State is the
	// board lifecycle, Phase is agent progress inside it. Empty for cards not
	// driven by the agent backend.
	Phase          string          `yaml:"phase,omitempty"            json:"phase,omitempty"`
	TokenUsage     *TokenUsage     `yaml:"token_usage,omitempty"      json:"token_usage,omitempty"`
	UsageBreakdown []UsageBucket   `yaml:"usage_breakdown,omitempty"  json:"usage_breakdown,omitempty"`
	Created        time.Time       `yaml:"created"                    json:"created"`
	Updated        time.Time       `yaml:"updated"                    json:"updated"`
	ActivityLog    []ActivityEntry `yaml:"activity_log,omitempty"     json:"activity_log,omitempty"`
	Body           string          `yaml:"-"                          json:"body"`
}

// ActivityEntry represents a log entry from an agent working on a card.
type ActivityEntry struct {
	Agent     string    `yaml:"agent"           json:"agent"`
	Timestamp time.Time `yaml:"ts"              json:"ts"`
	Action    string    `yaml:"action"          json:"action"`
	Message   string    `yaml:"message"         json:"message"`
	Skill     string    `yaml:"skill,omitempty" json:"skill,omitempty"` // set for skill_engaged actions
}

// Source tracks the external origin of imported cards.
type Source struct {
	System      string `yaml:"system"       json:"system"`
	ExternalID  string `yaml:"external_id"  json:"external_id"`
	ExternalURL string `yaml:"external_url" json:"external_url"`
}

// TokenUsage tracks cumulative token consumption and estimated cost for a card.
type TokenUsage struct {
	Model               string  `yaml:"model,omitempty"                json:"model,omitempty"`
	PromptTokens        int64   `yaml:"prompt_tokens"                  json:"prompt_tokens"`
	CompletionTokens    int64   `yaml:"completion_tokens"              json:"completion_tokens"`
	CacheReadTokens     int64   `yaml:"cache_read_tokens,omitempty"    json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int64   `yaml:"cache_creation_tokens,omitempty" json:"cache_creation_tokens,omitempty"`
	EstimatedCostUSD    float64 `yaml:"estimated_cost_usd"             json:"estimated_cost_usd"`
}

// UsageBucket is one merged (agent, model) row of token/cost attribution.
// CostSource records whether CostUSD came from the provider (actual) or the
// local rate table (estimated); recalculation must never overwrite actual.
type UsageBucket struct {
	Agent               string  `yaml:"agent"                           json:"agent"`
	Model               string  `yaml:"model"                           json:"model"`
	PromptTokens        int64   `yaml:"prompt_tokens"                   json:"prompt_tokens"`
	CompletionTokens    int64   `yaml:"completion_tokens"               json:"completion_tokens"`
	CacheReadTokens     int64   `yaml:"cache_read_tokens,omitempty"     json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int64   `yaml:"cache_creation_tokens,omitempty" json:"cache_creation_tokens,omitempty"`
	CostUSD             float64 `yaml:"cost_usd"                        json:"cost_usd"`
	CostSource          string  `yaml:"cost_source"                     json:"cost_source"`
}

var (
	// ErrMissingFrontmatter is returned when a card file lacks YAML frontmatter.
	ErrMissingFrontmatter = errors.New("missing YAML frontmatter")
	// ErrMalformedFrontmatter is returned when frontmatter cannot be parsed.
	ErrMalformedFrontmatter = errors.New("malformed YAML frontmatter")
)

// maxCardSize is the upper bound for card file input to prevent alias-bomb
// expansion and runaway allocations during YAML parsing (2 MiB).
const maxCardSize = 2 * 1024 * 1024

// ValidPhase reports whether p is a recognized orchestrator phase. Empty
// clears the field and is always valid.
func ValidPhase(p string) bool {
	switch p {
	case "", "plan", "execute", "judge", "document", "review", "integrate", "done":
		return true
	}

	return false
}

// frontmatterDelimiter is the YAML frontmatter boundary marker used for
// the opening delimiter (must appear at the very start of the file).
var frontmatterDelimiter = []byte("---")

// frontmatterClose is the closing delimiter that must be preceded by a
// newline so that literal "---" inside quoted YAML values or the body does
// not split the document unexpectedly.
var frontmatterClose = []byte("\n---")

// ParseCard parses a card file containing YAML frontmatter and markdown body.
// The file format is:
//
//	---
//	yaml frontmatter
//	---
//	markdown body
func ParseCard(data []byte) (*Card, error) {
	// Reject inputs that exceed the size limit before any allocation.
	if len(data) > maxCardSize {
		return nil, fmt.Errorf("%w: file exceeds maximum size of %d bytes", ErrMalformedFrontmatter, maxCardSize)
	}

	// Normalize line endings: convert \r\n to \n
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))

	// The file must start with "---\n" (the opening delimiter).
	if !bytes.HasPrefix(data, append(frontmatterDelimiter, '\n')) {
		return nil, ErrMissingFrontmatter
	}

	// Skip past the opening "---\n".
	rest := data[4:]

	// Find the closing "\n---" delimiter so that "---" embedded inside
	// quoted YAML values or the body section does not split the document.
	before, after, ok := bytes.Cut(rest, frontmatterClose)
	if !ok {
		return nil, ErrMissingFrontmatter
	}

	yamlContent := bytes.TrimSpace(before)
	if len(yamlContent) == 0 {
		return nil, ErrMissingFrontmatter
	}

	var card Card
	if err := yaml.Unmarshal(yamlContent, &card); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedFrontmatter, err)
	}

	// Everything after "\n---" is the body; consume the delimiter itself
	// plus any single trailing newline that follows it.
	body := after
	if len(body) > 0 && body[0] == '\n' {
		body = body[1:]
	}

	card.Body = string(body)

	return &card, nil
}

// SerializeCard converts a Card to its file representation with YAML frontmatter.
func SerializeCard(card *Card) ([]byte, error) {
	var buf bytes.Buffer

	// Write opening delimiter
	buf.WriteString("---\n")

	// Marshal YAML frontmatter
	yamlData, err := yaml.Marshal(card)
	if err != nil {
		return nil, fmt.Errorf("marshal card: %w", err)
	}

	buf.Write(yamlData)

	// Write closing delimiter
	buf.WriteString("---\n")

	// Write body
	buf.WriteString(card.Body)

	// Ensure trailing newline
	result := buf.Bytes()
	if len(result) > 0 && result[len(result)-1] != '\n' {
		result = append(result, '\n')
	}

	return result, nil
}
