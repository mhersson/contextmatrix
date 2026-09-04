package mcp

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/images"
	"github.com/mhersson/contextmatrix/internal/service"
)

// registerToolsConfig bundles the dependencies for registerTools. Mirrors the
// ServerConfig pattern used by NewServer so the registration surface can grow
// without churning callers.
type registerToolsConfig struct {
	Server            *mcp.Server
	Service           *service.CardService
	WorkflowSkillsDir string
	ImageStore        images.Store
	Blacklist         BlacklistWriter
	Outcomes          OutcomeWriter
	Bus               *events.Bus
	AwaitMax          time.Duration
	Playbooks         *service.PlaybookService
}

// registerTools adds all MCP tools to the server.
func registerTools(cfg registerToolsConfig) {
	server, svc := cfg.Server, cfg.Service

	registerListProjects(server, svc)
	registerListCards(server, svc)
	registerGetCard(server, svc, cfg.ImageStore)
	registerCreateCard(server, svc)
	registerUpdateCard(server, svc)
	registerTransitionCard(server, svc)
	registerClaimCard(server, svc)
	registerReleaseCard(server, svc)
	registerHeartbeat(server, svc)
	registerAddLog(server, svc)
	registerGetTaskContext(server, svc, cfg.ImageStore)
	registerCompleteTask(server, svc)
	registerGetSubtaskSummary(server, svc)
	registerCheckAgentHealth(server, svc)
	registerGetReadyTasks(server, svc)
	registerReportUsage(server, svc)
	registerRecalculateCosts(server, svc)
	registerCreateProject(server, svc)
	registerUpdateProject(server, svc)
	registerDeleteProject(server, svc)
	registerStartWorkflow(server, svc, cfg.WorkflowSkillsDir)
	registerStartReview(server, svc, cfg.WorkflowSkillsDir)
	registerGetSkill(server, svc, cfg.WorkflowSkillsDir)
	registerReportPush(server, svc)
	registerReportParked(server, svc)
	registerIncrementReviewAttempts(server, svc)
	registerPromoteToAutonomous(server, svc)

	if cfg.Bus != nil {
		registerAwaitSubtasks(server, svc, cfg.Bus, cfg.AwaitMax)
	}

	if cfg.Blacklist != nil {
		registerReportIncapableModel(server, cfg.Blacklist)
	}

	if cfg.Outcomes != nil {
		registerReportModelOutcome(server, svc, cfg.Outcomes)
	}

	if cfg.Playbooks != nil {
		registerPlaybookTools(server, cfg.Playbooks)
	}
}

// resolveProject resolves the project for a card ID when project is not provided.
// If project is already set, it returns it unchanged.
// If project is empty, it searches all projects for the card.
func resolveProject(ctx context.Context, svc *service.CardService, project, cardID string) (string, error) {
	if project != "" {
		return project, nil
	}

	_, proj, err := findCard(ctx, svc, cardID)
	if err != nil {
		return "", fmt.Errorf("resolve project for %s: %w", cardID, err)
	}

	return proj, nil
}

// requireHumanAgent fails fast when a tool is restricted to human callers and
// the supplied agent_id does not carry the "human:" prefix. The service layer
// also enforces this for promote_to_autonomous (defence in depth); the
// handler-level check ensures every human-only tool rejects in the same way
// with the same error shape.
func requireHumanAgent(agentID, toolName string) error {
	if !board.IsHumanAgentID(agentID) {
		return fmt.Errorf("%s is human-only (agent_id must start with 'human:' and have a non-empty suffix)", toolName)
	}

	return nil
}

// requireActiveClaim ensures the caller currently owns the card. Returns an
// error when the card is unclaimed or claimed by a different agent. Mirrors
// requireHumanAgent for ownership gating.
func requireActiveClaim(ctx context.Context, svc *service.CardService, project, cardID, agentID, toolName string) error {
	card, err := svc.GetCard(ctx, project, cardID)
	if err != nil {
		return fmt.Errorf("%s: load card %s: %w", toolName, cardID, err)
	}

	if card.AssignedAgent == "" {
		return fmt.Errorf("%s: card %s is not claimed; %s requires an active claim (call claim_card first)", toolName, cardID, toolName)
	}

	if !svc.OwnsClaim(card, agentID) {
		if svc.ClaimedElsewhere(card) {
			return fmt.Errorf("%s: card %s is claimed by %s via instance %s, not through this instance",
				toolName, cardID, card.AssignedAgent, card.ClaimedVia)
		}

		return fmt.Errorf("%s: card %s is claimed by %s, not %s", toolName, cardID, card.AssignedAgent, agentID)
	}

	return nil
}

// remoteErr puts the stable prefix the workflow skills retry on in front of
// a push-verified mutation's remote failure. Every other error passes through.
func remoteErr(err error) error {
	if errors.Is(err, service.ErrRemoteUnreachable) {
		return fmt.Errorf("remote unreachable: %w", err)
	}

	return err
}

// --- get_skill schema types (shared with tools_workflow.go) ---

type getSkillInput struct {
	// The jsonschema tag is a compile-time string and cannot be derived from
	// skillBuilders directly. assertGetSkillSchemaInSync (below) compares it
	// to skillNameSchemaDescription at package init time and panics on drift,
	// so adding a skill in one place but not the other fails fast in tests.
	SkillName       string `json:"skill_name" jsonschema:"required,skill name: brainstorming, create-plan, create-task, document-task, execute-task, init-project, plan-draft, review-task, run-autonomous, systematic-debugging"`
	CardID          string `json:"card_id,omitempty" jsonschema:"card ID (required for create-plan, execute-task, review-task, document-task, brainstorming, systematic-debugging, plan-draft)"`
	Description     string `json:"description,omitempty" jsonschema:"free-text description (used by create-task)"`
	Name            string `json:"name,omitempty" jsonschema:"project name (used by init-project)"`
	CallerModel     string `json:"caller_model,omitempty" jsonschema:"your model's short name or family (e.g. sonnet, opus on Claude harnesses) - enables inline execution when matching the skill model"`
	IncludePreamble *bool  `json:"include_preamble,omitempty" jsonschema:"include workflow rules preamble (default true, pass false to skip on subsequent calls when you already have it)"`
	IncludeCard     *bool  `json:"include_card,omitempty" jsonschema:"include the card body in the skill content (default true; pass false when you already hold the body from get_card)"`
}

// assertGetSkillSchemaInSync enforces that the jsonschema description on
// getSkillInput.SkillName matches skillNameSchemaDescription (derived from
// skillBuilders). Because struct tags must be compile-time literals, we
// cannot directly interpolate the derived list; this guard fires at package
// init time so any drift is caught immediately by `go test ./internal/mcp/...`.
func init() {
	assertGetSkillSchemaInSync()
}

func assertGetSkillSchemaInSync() {
	t := reflect.TypeFor[getSkillInput]()

	field, ok := t.FieldByName("SkillName")
	if !ok {
		panic("mcp: getSkillInput has no SkillName field")
	}

	got := field.Tag.Get("jsonschema")
	if got != skillNameSchemaDescription {
		panic(fmt.Sprintf(
			"mcp: getSkillInput.SkillName jsonschema tag drifted from skillBuilders.\n  tag:      %q\n  expected: %q\n  update one to match the other.",
			got, skillNameSchemaDescription,
		))
	}
}

type getSkillOutput struct {
	SkillName string `json:"skill_name"`
	Model     string `json:"model,omitempty"`
	Content   string `json:"content"`
	Inline    bool   `json:"inline,omitempty"`
}
