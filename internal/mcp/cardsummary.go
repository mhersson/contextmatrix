package mcp

import (
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
)

// CardSummary is board.Card without Body, ActivityLog, and UsageBreakdown -
// the three unbounded fields. Mutation and list tools return it instead of
// the full card so agent contexts do not re-absorb the whole spec (or an
// ever-growing per-(agent, model) usage ledger) on every call; get_card and
// get_task_context remain the full fetch. A dedicated type (rather than a
// body-cleared card copy) keeps "body" out of the JSON and the advertised
// output schema entirely - an empty-string body would be indistinguishable
// from a card with no body. Field parity with board.Card is enforced by
// TestCardSummaryMirrorsBoardCard.
type CardSummary struct {
	ID                      string              `json:"id"`
	Title                   string              `json:"title"`
	Project                 string              `json:"project"`
	Type                    string              `json:"type"`
	State                   string              `json:"state"`
	Priority                string              `json:"priority"`
	AssignedAgent           string              `json:"assigned_agent,omitempty"`
	LastHeartbeat           *time.Time          `json:"last_heartbeat,omitempty"`
	ClaimedVia              string              `json:"claimed_via,omitempty"`
	ClaimedAt               *time.Time          `json:"claimed_at,omitempty"`
	ClaimEpoch              int                 `json:"claim_epoch,omitempty"`
	Parent                  string              `json:"parent,omitempty"`
	Subtasks                []string            `json:"subtasks,omitempty"`
	DependsOn               []string            `json:"depends_on,omitempty"`
	DependenciesMet         *bool               `json:"dependencies_met,omitempty"`
	Context                 []string            `json:"context,omitempty"`
	Labels                  []string            `json:"labels,omitempty"`
	Skills                  *[]string           `json:"skills,omitempty"`
	Source                  *board.Source       `json:"source,omitempty"`
	Custom                  map[string]any      `json:"custom,omitempty"`
	Assignee                string              `json:"assignee,omitempty"`
	Autonomous              bool                `json:"autonomous"`
	ModelOrchestrator       string              `json:"model_orchestrator,omitempty"`
	ModelCoder              string              `json:"model_coder,omitempty"`
	ModelReviewer           string              `json:"model_reviewer,omitempty"`
	BestOfN                 int                 `json:"best_of_n,omitempty"`
	MaxCapability           bool                `json:"max_capability,omitempty"`
	MobParticipants         int                 `json:"mob_participants,omitempty"`
	MobPhases               []string            `json:"mob_phases,omitempty"`
	MobGuests               []string            `json:"mob_guests,omitempty"`
	Verify                  *board.VerifyConfig `json:"verify,omitempty"`
	Vetted                  bool                `json:"vetted"`
	CreatePR                bool                `json:"create_pr,omitempty"`
	AwaitCI                 bool                `json:"await_ci,omitempty"`
	AwaitCopilotReview      bool                `json:"await_copilot_review,omitempty"`
	BranchName              string              `json:"branch_name,omitempty"`
	BaseBranch              string              `json:"base_branch,omitempty"`
	PRUrl                   string              `json:"pr_url,omitempty"`
	ReviewAttempts          int                 `json:"review_attempts,omitempty"`
	WorkerStatus            string              `json:"worker_status,omitempty"`
	Phase                   string              `json:"phase,omitempty"`
	TokenUsage              *board.TokenUsage   `json:"token_usage,omitempty"`
	SubtaskCostUSD          float64             `json:"subtask_cost_usd,omitempty"`
	SubtaskCostHasEstimates bool                `json:"subtask_cost_has_estimates,omitempty"`
	InPlaybooks             []string            `json:"in_playbooks,omitempty"`
	Created                 time.Time           `json:"created"`
	Updated                 time.Time           `json:"updated"`
}

// summarizeCard copies shallowly - slice and pointer fields share backing
// storage with the source card, which is safe because results are serialized
// immediately and never mutated.
func summarizeCard(c *board.Card) *CardSummary {
	if c == nil {
		return nil
	}

	return &CardSummary{
		ID:                      c.ID,
		Title:                   c.Title,
		Project:                 c.Project,
		Type:                    c.Type,
		State:                   c.State,
		Priority:                c.Priority,
		AssignedAgent:           c.AssignedAgent,
		LastHeartbeat:           c.LastHeartbeat,
		ClaimedVia:              c.ClaimedVia,
		ClaimedAt:               c.ClaimedAt,
		ClaimEpoch:              c.ClaimEpoch,
		Parent:                  c.Parent,
		Subtasks:                c.Subtasks,
		DependsOn:               c.DependsOn,
		DependenciesMet:         c.DependenciesMet,
		Context:                 c.Context,
		Labels:                  c.Labels,
		Skills:                  c.Skills,
		Source:                  c.Source,
		Custom:                  c.Custom,
		Assignee:                c.Assignee,
		Autonomous:              c.Autonomous,
		ModelOrchestrator:       c.ModelOrchestrator,
		ModelCoder:              c.ModelCoder,
		ModelReviewer:           c.ModelReviewer,
		BestOfN:                 c.BestOfN,
		MaxCapability:           c.MaxCapability,
		MobParticipants:         c.MobParticipants,
		MobPhases:               c.MobPhases,
		MobGuests:               c.MobGuests,
		Verify:                  c.Verify,
		Vetted:                  c.Vetted,
		CreatePR:                c.CreatePR,
		AwaitCI:                 c.AwaitCI,
		AwaitCopilotReview:      c.AwaitCopilotReview,
		BranchName:              c.BranchName,
		BaseBranch:              c.BaseBranch,
		PRUrl:                   c.PRUrl,
		ReviewAttempts:          c.ReviewAttempts,
		WorkerStatus:            c.WorkerStatus,
		Phase:                   c.Phase,
		TokenUsage:              c.TokenUsage,
		SubtaskCostUSD:          c.SubtaskCostUSD,
		SubtaskCostHasEstimates: c.SubtaskCostHasEstimates,
		InPlaybooks:             c.InPlaybooks,
		Created:                 c.Created,
		Updated:                 c.Updated,
	}
}

// summarizeCards preserves nil vs empty: a non-nil empty input stays a
// non-nil empty slice so tools keep emitting "cards": [] on the wire.
func summarizeCards(cards []*board.Card) []*CardSummary {
	if cards == nil {
		return nil
	}

	out := make([]*CardSummary, len(cards))
	for i, c := range cards {
		out[i] = summarizeCard(c)
	}

	return out
}
