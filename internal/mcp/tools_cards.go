package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/images"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

type listCardsInput struct {
	Project string `json:"project" jsonschema:"required,project name"`
	State   string `json:"state,omitempty" jsonschema:"filter by state"`
	Type    string `json:"type,omitempty" jsonschema:"filter by card type"`
	Label   string `json:"label,omitempty" jsonschema:"filter by label"`
	Agent   string `json:"agent,omitempty" jsonschema:"filter by assigned agent"`
	Parent  string `json:"parent,omitempty" jsonschema:"filter by parent card ID"`
	AgentID string `json:"agent_id,omitempty" jsonschema:"caller identity (accepted for client parity; results are card summaries with no body)"`
}
type listCardsOutput struct {
	Cards []*CardSummary `json:"cards"`
}

type getCardInput struct {
	Project            string   `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID             string   `json:"card_id" jsonschema:"required,card ID (e.g. ALPHA-001)"`
	AgentID            string   `json:"agent_id,omitempty" jsonschema:"caller identity - unvetted external card bodies are redacted for non-human callers"`
	IncludeImages      *bool    `json:"include_images,omitempty" jsonschema:"attach inline image bytes for cm-server-hosted markdown image references in the body (default true; capped at 10 images per call and ~20 MiB cumulative bytes, with later references in body order omitted when over budget)"`
	IncludeActivityLog *bool    `json:"include_activity_log,omitempty" jsonschema:"include the activity log (default true; pass false to trim ~half of a long-lived card's payload)"`
	Sections           []string `json:"sections,omitempty" jsonschema:"return only these H2 body sections (exact heading text without ##, e.g. 'Plan'; pass 'intro' to include the pre-heading text); unmatched names yield an empty body, not the full one"`
}

type createCardInput struct {
	Project   string    `json:"project" jsonschema:"required,project name"`
	Title     string    `json:"title" jsonschema:"required,card title"`
	Type      string    `json:"type" jsonschema:"required,card type (task/bug/feature). Overridden to 'subtask' when parent is set."`
	Priority  string    `json:"priority" jsonschema:"required,priority (low/medium/high/critical)"`
	Labels    []string  `json:"labels,omitempty" jsonschema:"optional labels"`
	Skills    *[]string `json:"skills,omitempty" jsonschema:"optional task-skill names to mount in the worker container; nil inherits from parent or project default, [] means none, [list] constrains"`
	Body      string    `json:"body,omitempty" jsonschema:"optional markdown body"`
	Parent    string    `json:"parent,omitempty" jsonschema:"parent card ID for subtasks"`
	DependsOn []string  `json:"depends_on,omitempty" jsonschema:"card IDs this depends on"`
	// AgentID is accepted for parity with the other card tools: the agent MCP
	// client injects agent_id into every call, so create_card must declare it or
	// the strict (additionalProperties:false) schema rejects the orchestrator's
	// subtask creation. Threaded into the self_containment_warning activity
	// entry's Agent field when the lint finds something to flag.
	AgentID string `json:"agent_id,omitempty" jsonschema:"caller identity (attributed on the self_containment_warning activity entry when the lint flags the body)"`
}

// NOTE: vetted, create_pr, await_ci, await_copilot_review,
// base_branch, best_of_n, max_capability, assignee, the mob session fields
// (mob_participants, mob_phases, mob_guests), and
// model pin fields (model_orchestrator, model_coder, model_reviewer) are
// intentionally excluded - they are human-only fields. Model pins are excluded
// for the same reason: they express human intent about which model to use and
// must not be overridden by the agent that is itself subject to the pin.
// Assignee names a responsible human, which only a human can decide.
// MaxCapability controls cost vs. capability steering, also a human decision.
type updateCardInput struct {
	Project   string    `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID    string    `json:"card_id" jsonschema:"required,card ID"`
	AgentID   string    `json:"agent_id,omitempty" jsonschema:"agent performing the update - if set and card is claimed by a different agent, returns ErrAgentMismatch"`
	Title     *string   `json:"title,omitempty" jsonschema:"new title"`
	Priority  *string   `json:"priority,omitempty" jsonschema:"new priority"`
	Labels    []string  `json:"labels,omitempty" jsonschema:"new labels (replaces all)"`
	DependsOn []string  `json:"depends_on,omitempty" jsonschema:"card IDs this depends on; replaces the list; [] clears; omit to leave unchanged"`
	Skills    *[]string `json:"skills,omitempty" jsonschema:"new task skills (replaces all); [] means none, omit to leave unchanged"`
	Body      *string   `json:"body,omitempty" jsonschema:"new markdown body"`
	Phase     *string   `json:"phase,omitempty" jsonschema:"orchestrator phase: plan|execute|judge|document|review|integrate|pr_gates|done; empty clears"`
	// UpsertSectionHeading and UpsertSectionContent must be provided together
	// (or neither): replace-or-append one H2 section without resending the
	// whole body. Mutually exclusive with Body - enforced by the service layer.
	UpsertSectionHeading *string `json:"upsert_section_heading,omitempty" jsonschema:"H2 heading (without ##) to replace or append; use with upsert_section_content instead of body to avoid re-sending the whole body"`
	UpsertSectionContent *string `json:"upsert_section_content,omitempty" jsonschema:"markdown content for the section named by upsert_section_heading"`
	Autonomous           *bool   `json:"autonomous,omitempty" jsonschema:"set the autonomous mode flag on the card"`
}

type transitionCardInput struct {
	Project  string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID   string `json:"card_id" jsonschema:"required,card ID"`
	AgentID  string `json:"agent_id,omitempty" jsonschema:"agent performing the transition - if set and card is claimed by a different agent, returns ErrAgentMismatch"`
	NewState string `json:"new_state" jsonschema:"required,target state"`
}

type getTaskContextInput struct {
	Project       string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID        string `json:"card_id" jsonschema:"required,card ID"`
	AgentID       string `json:"agent_id,omitempty" jsonschema:"caller identity - unvetted external card bodies are redacted for non-human callers"`
	IncludeImages *bool  `json:"include_images,omitempty" jsonschema:"attach inline image bytes for cm-server-hosted markdown image references in the primary card body (default true; capped at 10 images per call and ~20 MiB cumulative bytes, with later references in body order omitted when over budget; siblings stay text-only)"`
}
type getTaskContextOutput struct {
	Card     *board.Card          `json:"card"`
	Parent   *board.Card          `json:"parent,omitempty"`
	Siblings []*CardSummary       `json:"siblings,omitempty"`
	Config   *board.ProjectConfig `json:"config"`
}

type getSubtaskSummaryInput struct {
	Project  string `json:"project,omitempty" jsonschema:"project name (resolved from parent ID if omitted)"`
	ParentID string `json:"parent_id" jsonschema:"required,parent card ID"`
}
type getSubtaskSummaryOutput struct {
	ParentID string         `json:"parent_id"`
	Total    int            `json:"total"`
	Counts   map[string]int `json:"counts"`
}

type checkAgentHealthInput struct {
	Project  string `json:"project,omitempty" jsonschema:"project name (resolved from parent ID if omitted)"`
	ParentID string `json:"parent_id" jsonschema:"required,parent card ID whose subtasks to check"`
}

// AgentHealthStatus represents the computed health of a single subtask's agent.
type AgentHealthStatus struct {
	CardID            string `json:"card_id"`
	Title             string `json:"title"`
	State             string `json:"state"`
	AssignedAgent     string `json:"assigned_agent,omitempty"`
	LastHeartbeat     string `json:"last_heartbeat,omitempty"`
	SecondsSinceHbeat *int64 `json:"seconds_since_heartbeat,omitempty"`
	Status            string `json:"status"` // active, warning, stalled, unassigned, completed
}

type checkAgentHealthOutput struct {
	ParentID       string              `json:"parent_id"`
	TimeoutSeconds int64               `json:"timeout_seconds"`
	WarningSeconds int64               `json:"warning_seconds"`
	Subtasks       []AgentHealthStatus `json:"subtasks"`
	Summary        string              `json:"summary"`
}

type getReadyTasksInput struct {
	Project  string `json:"project" jsonschema:"required,project name"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"optional parent card ID to scope search"`
}
type getReadyTasksOutput struct {
	Cards []*CardSummary `json:"cards"`
}

type reportUsageInput struct {
	Project             string   `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID              string   `json:"card_id" jsonschema:"required,card ID"`
	AgentID             string   `json:"agent_id" jsonschema:"required,agent ID reporting usage"`
	OnBehalfOf          string   `json:"on_behalf_of,omitempty" jsonschema:"attribute this usage to a different agent identity (e.g. a subagent) while agent_id satisfies the claim check"`
	Model               string   `json:"model,omitempty" jsonschema:"model that actually served the calls (never derived from the agent name); used for cost calculation (e.g. claude-sonnet-4)"`
	PromptTokens        int64    `json:"prompt_tokens" jsonschema:"required,number of prompt tokens used"`
	CompletionTokens    int64    `json:"completion_tokens" jsonschema:"required,number of completion tokens used"`
	CacheReadTokens     int64    `json:"cache_read_tokens,omitempty" jsonschema:"number of cache-read tokens (billed at 0.10× base input rate)"`
	CacheCreationTokens int64    `json:"cache_creation_tokens,omitempty" jsonschema:"number of cache-creation tokens (billed at 1.25× base input rate)"`
	ActualCostUSD       *float64 `json:"actual_cost_usd,omitempty" jsonschema:"authoritative provider-reported cost in USD for this delta; omit to use the server rate table"`
	Source              string   `json:"source,omitempty" jsonschema:"who produced the numbers: self (default, agent-estimated) or collector (measured from real usage frames)"`
	Phase               string   `json:"phase,omitempty" jsonschema:"FSM phase this usage belongs to (plan|execute|judge|document|review|integrate|pr_gates|done); omit to use the card's current phase"`
	Step                string   `json:"step,omitempty" jsonschema:"model-call kind within the phase (main|gate|brainstorm|verify_propose|mob_seat|mob_moderator|checkpoint|judge); omit for the primary phase call"`
	DurationMS          int64    `json:"duration_ms,omitempty" jsonschema:"wall time of the model step in milliseconds; used for latency metrics only"`
}

type recalculateCostsInput struct {
	Project      string `json:"project" jsonschema:"required,project name"`
	DefaultModel string `json:"default_model" jsonschema:"required,model name used when card has no stored model (e.g. claude-sonnet-4-6)"`
}

type recalculateCostsOutput struct {
	CardsUpdated          int     `json:"cards_updated"`
	TotalCostRecalculated float64 `json:"total_cost_recalculated"`
}

type reportPushInput struct {
	Project string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID  string `json:"card_id" jsonschema:"required,card ID"`
	AgentID string `json:"agent_id" jsonschema:"required,agent ID"`
	Branch  string `json:"branch" jsonschema:"required,git branch that was pushed to"`
	PRUrl   string `json:"pr_url,omitempty" jsonschema:"pull request URL if created"`
}

type reportPushOutput struct {
	Card *CardSummary `json:"card"`
}

type reportParkedInput struct {
	Project string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID  string `json:"card_id" jsonschema:"required,card ID"`
	AgentID string `json:"agent_id" jsonschema:"required,agent ID"`
	Reason  string `json:"reason" jsonschema:"required,why the run parked the card, e.g. review parked: attempts cap exhausted without approval"`
}

type reportParkedOutput struct {
	Card *CardSummary `json:"card"`
}

type promoteToAutonomousInput struct {
	Project string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID  string `json:"card_id" jsonschema:"required,card ID"`
	AgentID string `json:"agent_id" jsonschema:"required,agent ID performing the promotion"`
}

func registerListCards(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_cards",
		Description: "List cards in a project, optionally filtered by state, type, label, agent, or parent. Returns card summaries without body or activity_log; use get_card for full content. Survey tool: browse and select in a separate session from card execution - start execution fresh with just a card ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input listCardsInput) (*mcp.CallToolResult, listCardsOutput, error) {
		filter := storage.CardFilter{
			State:         input.State,
			Type:          input.Type,
			Label:         input.Label,
			AssignedAgent: input.Agent,
			Parent:        input.Parent,
		}

		cards, err := svc.ListCards(ctx, input.Project, filter)
		if err != nil {
			return nil, listCardsOutput{}, fmt.Errorf("list cards: %w", err)
		}

		if cards == nil {
			cards = []*board.Card{}
		}

		// No body redaction needed: summaries structurally cannot carry a
		// body, so unvetted external card bodies never reach agent context
		// through this tool (see TestCardSummaryMirrorsBoardCard).
		return nil, listCardsOutput{Cards: summarizeCards(cards)}, nil
	})
}

func registerGetCard(server *mcp.Server, svc *service.CardService, imageStore images.Store) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_card",
		Description: "Get a single card by ID, including its full body and metadata. By default, attaches inline image bytes for any cm-server-hosted markdown images in the body (capped at 10); pass include_images=false to skip. Cumulative attached image bytes are capped at ~20 MiB; later references in body order are omitted when over budget. " +
			"Pass include_activity_log=false to drop the activity log, which is often the larger half of a long-lived card's payload. " +
			"Pass sections to return only the named H2 body sections instead of the full body; a sections request that matches nothing returns body=\"\" rather than the full body. Image attachment scans the filtered body, so images referenced only by an omitted section are not attached.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getCardInput) (*mcp.CallToolResult, *board.Card, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		card, err := svc.GetCard(ctx, project, input.CardID)
		if err != nil {
			return nil, nil, fmt.Errorf("get card %s: %w", input.CardID, err)
		}

		// Redact unvetted card body for non-human callers so prompt-injection
		// payloads from imported external cards cannot reach agent context.
		card = redactCardForAgent(card, input.AgentID)

		// Shallow-copy before trimming (precedent: vetting.go's
		// redactCardForAgent) so the store-backed card returned above is
		// never mutated in place.
		trimmed := *card

		if input.IncludeActivityLog != nil && !*input.IncludeActivityLog {
			trimmed.ActivityLog = nil
		}

		if len(input.Sections) > 0 {
			trimmed.Body = filterBodySectionsExact(trimmed.Body, prefixSectionKeep(input.Sections))
		}

		// attachImagesToResult must scan the trimmed body: a sections filter
		// can drop the only paragraph referencing an image, and that image
		// must not be attached once its section is gone.
		result := attachImagesToResult(ctx, imageStore,
			attachContext{Tool: "get_card", CardID: trimmed.ID},
			&trimmed, trimmed.Body, input.IncludeImages, 0,
		)

		return result, &trimmed, nil
	})
}

// prefixSectionKeep prepares get_card's caller-supplied section names for
// filterBodySectionsExact: heading names arrive without "## " and get it
// prepended, matching filterBodySections' keep-list convention. The literal
// pseudo-entry "intro" (case-insensitive) passes through unprefixed since it
// names the pre-heading text, not an H2.
func prefixSectionKeep(sections []string) []string {
	keep := make([]string, 0, len(sections))

	for _, s := range sections {
		if strings.EqualFold(strings.TrimSpace(s), "intro") {
			keep = append(keep, "intro")

			continue
		}

		keep = append(keep, "## "+s)
	}

	return keep
}

// cardMutationResult is the typed output of create_card and update_card: the
// card summary plus advisory self-containment warnings. Warnings never block
// the mutation - the caller is expected to fix the body via update_card.
type cardMutationResult struct {
	CardSummary
	Warnings []string `json:"warnings,omitempty"`
}

// foreignRepoRefs collects the repo URLs of every project EXCEPT project, for
// the self-containment lint. Best-effort: any error returns nil - the lint is
// advisory and must never fail a card mutation.
func foreignRepoRefs(ctx context.Context, svc *service.CardService, project string) []string {
	projects, err := svc.ListProjects(ctx)
	if err != nil {
		return nil
	}

	var refs []string

	for _, p := range projects {
		if p.Name == project {
			continue
		}

		if p.Repo != "" {
			refs = append(refs, p.Repo)
		}

		for _, r := range p.Repos {
			refs = append(refs, r.URL)
		}
	}

	return refs
}

// lintCardMutation returns the self-containment warnings text carries that
// previous does not, and records them as one advisory activity-log entry.
// Diffing against the pre-mutation text keeps a full-body rewrite - the
// agent's history writes - from re-warning about a signal already on the card.
func lintCardMutation(ctx context.Context, svc *service.CardService, project, cardID, agentID, verb, text, previous string) []string {
	repos := foreignRepoRefs(ctx, svc, project)

	seen := make(map[string]bool)
	for _, w := range board.LintSelfContained(previous, repos) {
		seen[w] = true
	}

	var warnings []string

	for _, w := range board.LintSelfContained(text, repos) {
		if !seen[w] {
			warnings = append(warnings, w)
		}
	}

	if len(warnings) == 0 {
		return nil
	}

	entry := board.ActivityEntry{
		Agent:   agentID,
		Action:  "self_containment_warning",
		Message: fmt.Sprintf("%s with %d self-containment warning(s) - body references the card author's environment", verb, len(warnings)),
	}
	_, _ = svc.AddLogEntry(ctx, project, cardID, entry) //nolint:errcheck // advisory note; must not fail the mutation

	return warnings
}

func lintText(card *board.Card) string {
	return card.Title + "\n" + card.Body
}

func registerCreateCard(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_card",
		Description: "Create a new card in a project. Returns a card summary with the generated ID (the body is stored but not echoed back). The card starts in the project's first state (usually 'todo'). IMPORTANT: After creation, the card must be claimed with claim_card before any work begins. Never start working on a card without claiming it first. EXECUTION CONTRACT: the card may be executed by an autonomous agent in a container holding only a fresh clone of the project repo - write the body self-contained (inline context; no local-machine paths, no other repos) with acceptance criteria verifiable inside that repo. One card = one deliverable; split independent deliverables into separate cards linked with depends_on. The response may include 'warnings' - fix them with update_card before proceeding.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input createCardInput) (*mcp.CallToolResult, *cardMutationResult, error) {
		// depends_on is part of CreateCardInput so create + dependency wiring
		// happen as a single atomic operation (one git commit, no race window
		// between create and follow-up update).
		svcInput := service.CreateCardInput{
			Title:     input.Title,
			Type:      input.Type,
			Priority:  input.Priority,
			Labels:    input.Labels,
			Skills:    input.Skills,
			Body:      input.Body,
			Parent:    input.Parent,
			DependsOn: input.DependsOn,
		}

		card, err := svc.CreateCard(ctx, input.Project, svcInput)
		if err != nil {
			return nil, nil, remoteErr(fmt.Errorf("create card: %w", err))
		}

		submitted := strings.Join([]string{input.Title, input.Body}, "\n")
		warnings := lintCardMutation(ctx, svc, input.Project, card.ID, input.AgentID, "created", submitted, "")

		return nil, &cardMutationResult{CardSummary: *summarizeCard(card), Warnings: warnings}, nil
	})
}

func registerUpdateCard(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "update_card",
		Description: "Update a card's mutable fields. Only provided fields are changed; omitted fields keep their current values. Does NOT change state - use transition_card for state changes. " +
			"Prefer upsert_section_heading/upsert_section_content for adding or updating one section - never re-send a body containing human-authored text just to append. The same execution contract as create_card applies: keep the body self-contained and acceptance criteria verifiable inside the project repo. The response may include 'warnings'.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input updateCardInput) (*mcp.CallToolResult, *cardMutationResult, error) {
		if (input.UpsertSectionHeading == nil) != (input.UpsertSectionContent == nil) {
			return nil, nil, fmt.Errorf("upsert_section_heading and upsert_section_content must be provided together")
		}

		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		patchInput := service.PatchCardInput{
			AgentID:    input.AgentID,
			Title:      input.Title,
			Priority:   input.Priority,
			Labels:     input.Labels,
			DependsOn:  input.DependsOn,
			Skills:     input.Skills,
			Body:       input.Body,
			Phase:      input.Phase,
			Autonomous: input.Autonomous,
		}

		if input.UpsertSectionHeading != nil {
			patchInput.UpsertSection = &service.SectionPatch{
				Heading: *input.UpsertSectionHeading,
				Content: *input.UpsertSectionContent,
			}
		}

		lintable := input.Title != nil || input.Body != nil || input.UpsertSectionContent != nil

		var before *board.Card

		if lintable {
			before, err = svc.GetCard(ctx, project, input.CardID)
			if err != nil {
				return nil, nil, fmt.Errorf("get card %s: %w", input.CardID, err)
			}
		}

		card, err := svc.PatchCard(ctx, project, input.CardID, patchInput)
		if err != nil {
			return nil, nil, fmt.Errorf("update card %s: %w", input.CardID, err)
		}

		var warnings []string
		if lintable {
			warnings = lintCardMutation(ctx, svc, project, card.ID, input.AgentID, "updated", lintText(card), lintText(before))
		}

		return nil, &cardMutationResult{CardSummary: *summarizeCard(card), Warnings: warnings}, nil
	})
}

func registerTransitionCard(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "transition_card",
		Description: "Change a card's state. Validates that the transition is allowed by the project's state machine. Returns 'invalid state transition' error with valid targets if not allowed.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input transitionCardInput) (*mcp.CallToolResult, *CardSummary, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		// Agent-ownership is enforced inside PatchCard via AgentID.
		patchInput := service.PatchCardInput{
			AgentID: input.AgentID,
			State:   &input.NewState,
		}

		card, err := svc.PatchCard(ctx, project, input.CardID, patchInput)
		if err != nil {
			return nil, nil, fmt.Errorf("transition card %s to %s: %w", input.CardID, input.NewState, err)
		}

		return nil, summarizeCard(card), nil
	})
}

func registerGetTaskContext(server *mcp.Server, svc *service.CardService, imageStore images.Store) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_task_context",
		Description: "Get a card with its parent card, sibling cards (same parent), and project config in a single call. Sub-agents should call this first before touching anything - it eliminates multiple round-trips. The primary card and parent are full; siblings are card summaries without body or activity_log (use get_card for a sibling's content). By default, attaches inline image bytes for any cm-server-hosted markdown images in the primary card body (capped at 10); pass include_images=false to skip. Cumulative attached image bytes are capped at ~20 MiB; later references in body order are omitted when over budget.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getTaskContextInput) (*mcp.CallToolResult, getTaskContextOutput, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, getTaskContextOutput{}, err
		}

		card, err := svc.GetCard(ctx, project, input.CardID)
		if err != nil {
			return nil, getTaskContextOutput{}, fmt.Errorf("get card %s: %w", input.CardID, err)
		}

		cfg, err := svc.GetProject(ctx, project)
		if err != nil {
			return nil, getTaskContextOutput{}, fmt.Errorf("get project config: %w", err)
		}

		// Redact unvetted card body for non-human callers - get_task_context
		// is the primary prompt-injection vector because its response is fed
		// straight into agent context.
		primary := redactCardForAgent(card, input.AgentID)

		out := getTaskContextOutput{
			Card:   primary,
			Config: cfg,
		}

		if card.Parent != "" {
			parent, err := svc.GetCard(ctx, project, card.Parent)
			if err == nil {
				out.Parent = redactCardForAgent(parent, input.AgentID)
			}
		}

		// Load siblings (cards with same parent). Summaries only: siblings
		// serve overlap awareness, and their bodies grow as sibling agents
		// write plans and findings - the N-1 full bodies were the dominant
		// payload here. Subtask detail is fetched per card via get_card.
		if card.Parent != "" {
			siblings, err := svc.ListCards(ctx, project, storage.CardFilter{Parent: card.Parent})
			if err == nil {
				// Exclude self from siblings
				filtered := make([]*board.Card, 0, len(siblings))
				for _, s := range siblings {
					if s.ID != card.ID {
						filtered = append(filtered, s)
					}
				}

				out.Siblings = summarizeCards(filtered)
			}
		}

		result := attachImagesToResult(ctx, imageStore,
			attachContext{Tool: "get_task_context", CardID: primary.ID},
			out, primary.Body, input.IncludeImages, 0,
		)

		return result, out, nil
	})
}

func registerGetSubtaskSummary(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_subtask_summary",
		Description: "Get counts of subtasks by state for a parent card. Returns {todo: N, in_progress: N, done: N, ...}. Use this to check if all subtasks are done before transitioning the parent.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSubtaskSummaryInput) (*mcp.CallToolResult, getSubtaskSummaryOutput, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.ParentID)
		if err != nil {
			return nil, getSubtaskSummaryOutput{}, err
		}

		cards, err := svc.ListCards(ctx, project, storage.CardFilter{Parent: input.ParentID})
		if err != nil {
			return nil, getSubtaskSummaryOutput{}, fmt.Errorf("list subtasks: %w", err)
		}

		counts := make(map[string]int)
		for _, card := range cards {
			counts[card.State]++
		}

		return nil, getSubtaskSummaryOutput{
			ParentID: input.ParentID,
			Total:    len(cards),
			Counts:   counts,
		}, nil
	})
}

func registerCheckAgentHealth(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_agent_health",
		Description: "Check health status of all subtask agents for a parent card. Returns heartbeat age and computed status (active/warning/stalled/unassigned/completed) for each subtask. Use this to detect dead sub-agents that need respawning.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input checkAgentHealthInput) (*mcp.CallToolResult, checkAgentHealthOutput, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.ParentID)
		if err != nil {
			return nil, checkAgentHealthOutput{}, err
		}

		cards, err := svc.ListCards(ctx, project, storage.CardFilter{Parent: input.ParentID})
		if err != nil {
			return nil, checkAgentHealthOutput{}, fmt.Errorf("list subtasks: %w", err)
		}

		timeout := svc.HeartbeatTimeout()
		warningThreshold := timeout / 2
		now := svc.Now()

		var (
			subtasks                                                []AgentHealthStatus
			stalledCount, warningCount, activeCount, completedCount int
		)

		for _, card := range cards {
			status := AgentHealthStatus{
				CardID:        card.ID,
				Title:         card.Title,
				State:         card.State,
				AssignedAgent: card.AssignedAgent,
			}

			switch {
			case card.State == board.StateDone || card.State == board.StateReview:
				status.Status = "completed"
				completedCount++
			case card.State == board.StateStalled:
				status.Status = "stalled"
				stalledCount++
			case card.AssignedAgent == "":
				status.Status = "unassigned"
			default:
				if card.LastHeartbeat != nil {
					status.LastHeartbeat = card.LastHeartbeat.Format(time.RFC3339)
					elapsed := int64(now.Sub(*card.LastHeartbeat).Seconds())
					status.SecondsSinceHbeat = &elapsed

					switch {
					case now.Sub(*card.LastHeartbeat) >= timeout:
						status.Status = "stalled"
						stalledCount++
					case now.Sub(*card.LastHeartbeat) >= warningThreshold:
						status.Status = "warning"
						warningCount++
					default:
						status.Status = "active"
						activeCount++
					}
				} else {
					status.Status = "warning"
					warningCount++
				}
			}

			subtasks = append(subtasks, status)
		}

		summary := fmt.Sprintf("%d active, %d warning, %d stalled, %d completed, %d total",
			activeCount, warningCount, stalledCount, completedCount, len(cards))

		return nil, checkAgentHealthOutput{
			ParentID:       input.ParentID,
			TimeoutSeconds: int64(timeout.Seconds()),
			WarningSeconds: int64(warningThreshold.Seconds()),
			Subtasks:       subtasks,
			Summary:        summary,
		}, nil
	})
}

func registerGetReadyTasks(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_ready_tasks",
		Description: "Get unclaimed 'todo' cards that are ready to start - all depends_on cards are in 'done' state. Optionally scoped to a parent card's subtasks. Use this to find which tasks can be started in parallel. Returns card summaries without body or activity_log; use get_card for full content. Survey tool: pick a card here, then start execution in a fresh session with just the card ID - survey output carried into an execution session is re-billed on every subsequent call.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getReadyTasksInput) (*mcp.CallToolResult, getReadyTasksOutput, error) {
		filter := storage.CardFilter{State: board.StateTodo}
		if input.ParentID != "" {
			filter.Parent = input.ParentID
		}

		cards, err := svc.ListCards(ctx, input.Project, filter)
		if err != nil {
			return nil, getReadyTasksOutput{}, fmt.Errorf("list todo cards: %w", err)
		}

		// Filter to unclaimed cards with all dependencies met
		// ListCards already computes DependenciesMet on each card
		ready := make([]*board.Card, 0)

		for _, card := range cards {
			if card.AssignedAgent != "" {
				continue // already claimed
			}

			if card.DependenciesMet != nil && !*card.DependenciesMet {
				continue
			}

			if card.Source != nil && !card.Vetted {
				continue // unvetted external cards cannot be claimed by agents
			}

			ready = append(ready, card)
		}

		return nil, getReadyTasksOutput{Cards: summarizeCards(ready)}, nil
	})
}

func registerReportUsage(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "report_usage",
		Description: "Report token usage for a card. Increments running totals of prompt and completion tokens, " +
			"and recalculates estimated cost based on the model's configured rates. " +
			"Accepts optional cache_read_tokens (billed at 0.10× base input rate) and " +
			"cache_creation_tokens (billed at 1.25× base input rate) for prompt-cache cost accounting. " +
			"Accepts on_behalf_of to attribute usage to a different agent identity (e.g. a subagent) " +
			"while agent_id still satisfies the claim check, and source to mark whether the counts are " +
			"self-estimated or collector-measured. Call this on heartbeat and when completing a task.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input reportUsageInput) (*mcp.CallToolResult, *CardSummary, error) {
		// Reject negative token counts at the handler boundary. The service
		// layer uses += on the running totals, so a negative value would
		// silently decrement counters and produce nonsensical totals.
		if input.PromptTokens < 0 || input.CompletionTokens < 0 {
			return nil, nil, fmt.Errorf("report usage for %s: tokens must be non-negative (prompt_tokens=%d, completion_tokens=%d)",
				input.CardID, input.PromptTokens, input.CompletionTokens)
		}

		if input.CacheReadTokens < 0 || input.CacheCreationTokens < 0 {
			return nil, nil, fmt.Errorf("report usage for %s: cache tokens must be non-negative (cache_read_tokens=%d, cache_creation_tokens=%d)",
				input.CardID, input.CacheReadTokens, input.CacheCreationTokens)
		}

		if input.ActualCostUSD != nil && *input.ActualCostUSD < 0 {
			return nil, nil, fmt.Errorf("report usage for %s: actual cost must be non-negative (actual_cost_usd=%v)",
				input.CardID, *input.ActualCostUSD)
		}

		if input.DurationMS < 0 {
			return nil, nil, fmt.Errorf("report usage for %s: duration must be non-negative (duration_ms=%d)",
				input.CardID, input.DurationMS)
		}

		if input.Source != "" && input.Source != "self" && input.Source != "collector" {
			return nil, nil, fmt.Errorf("report usage for %s: source must be \"self\" or \"collector\" (got %q)",
				input.CardID, input.Source)
		}

		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		card, err := svc.ReportUsage(ctx, project, input.CardID, service.ReportUsageInput{
			AgentID:             input.AgentID,
			OnBehalfOf:          input.OnBehalfOf,
			Model:               input.Model,
			PromptTokens:        input.PromptTokens,
			CompletionTokens:    input.CompletionTokens,
			CacheReadTokens:     input.CacheReadTokens,
			CacheCreationTokens: input.CacheCreationTokens,
			ActualCostUSD:       input.ActualCostUSD,
			Source:              input.Source,
			Phase:               input.Phase,
			Step:                input.Step,
			DurationMS:          input.DurationMS,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("report usage for %s: %w", input.CardID, err)
		}

		return nil, summarizeCard(card), nil
	})
}

func registerRecalculateCosts(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "recalculate_costs",
		Description: "Recompute estimated costs from the current rate table. Cards with a usage breakdown: every estimated bucket is re-priced (stale prices corrected); actual provider-reported costs are never modified. Legacy cards without a breakdown: fill-missing-only - cards with non-zero tokens but $0 cost get a cost, cards with an existing cost are not modified.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input recalculateCostsInput) (*mcp.CallToolResult, recalculateCostsOutput, error) {
		result, err := svc.RecalculateCosts(ctx, input.Project, input.DefaultModel)
		if err != nil {
			return nil, recalculateCostsOutput{}, fmt.Errorf("recalculate costs: %w", err)
		}

		return nil, recalculateCostsOutput{
			CardsUpdated:          result.CardsUpdated,
			TotalCostRecalculated: result.TotalCostRecalculated,
		}, nil
	})
}

// --- report_push tool ---

func registerReportPush(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "report_push",
		Description: "Report a completed git push. Call this AFTER pushing to record the branch and " +
			"optional PR URL on the card. Returns a hard error if the branch is main or master.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input reportPushInput) (*mcp.CallToolResult, reportPushOutput, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, reportPushOutput{}, err
		}

		branch := strings.TrimSpace(input.Branch)

		card, err := svc.RecordPush(ctx, project, input.CardID, input.AgentID, branch, input.PRUrl)
		if err != nil {
			return nil, reportPushOutput{}, fmt.Errorf("report push: %w", err)
		}

		return nil, reportPushOutput{Card: summarizeCard(card)}, nil
	})
}

// --- report_parked tool ---

func registerReportParked(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "report_parked",
		Description: "Report that this run is parking the card - review or PR gates left for a human. " +
			"Sets worker_status to \"parked\" (survives the run's completed callback; the next trigger clears it) " +
			"and records the reason in the activity log. Requires the claim: agent_id must match assigned_agent.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input reportParkedInput) (*mcp.CallToolResult, reportParkedOutput, error) {
		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, reportParkedOutput{}, err
		}

		card, err := svc.ReportParked(ctx, project, input.CardID, input.AgentID, strings.TrimSpace(input.Reason))
		if err != nil {
			return nil, reportParkedOutput{}, fmt.Errorf("report parked: %w", err)
		}

		return nil, reportParkedOutput{Card: summarizeCard(card)}, nil
	})
}

// --- promote_to_autonomous tool ---

func registerPromoteToAutonomous(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "promote_to_autonomous",
		Description: "Promote a card to autonomous mode by flipping its autonomous flag to true. " +
			"Human-only: agent_id must start with \"human:\" or the call is rejected. " +
			"Idempotent: calling on an already-autonomous card is a no-op. " +
			"Returns an error if the card is in a terminal state (done/not_planned). " +
			"Appends an activity log entry and fires an SSE event so the UI updates live.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input promoteToAutonomousInput) (*mcp.CallToolResult, *CardSummary, error) {
		// Defence in depth: the service layer rejects non-human callers, but
		// gate at the handler boundary too so the rejection style matches the
		// other human-only tools and the error never depends on project
		// resolution succeeding first.
		if err := requireHumanAgent(input.AgentID, "promote_to_autonomous"); err != nil {
			return nil, nil, err
		}

		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		card, err := svc.PromoteToAutonomous(ctx, project, input.CardID, input.AgentID)
		if err != nil {
			return nil, nil, fmt.Errorf("promote card %s to autonomous: %w", input.CardID, err)
		}

		return nil, summarizeCard(card), nil
	})
}
