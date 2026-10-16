package mcp

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// CardRunner starts an autonomous run of one card on the task backend.
type CardRunner func(ctx context.Context, project, cardID string) (*board.Card, error)

// RunnerSlot holds the CardRunner the router binds after the MCP server exists.
type RunnerSlot struct{ fn atomic.Pointer[CardRunner] }

// Set binds the runner.
func (s *RunnerSlot) Set(fn CardRunner) { s.fn.Store(&fn) }

// Get returns the bound runner, or nil before Set.
func (s *RunnerSlot) Get() CardRunner {
	if p := s.fn.Load(); p != nil {
		return *p
	}

	return nil
}

// --- run_card tool ---

type runCardInput struct {
	Project string `json:"project,omitempty" jsonschema:"project name (resolved from card ID if omitted)"`
	CardID  string `json:"card_id" jsonschema:"required,card ID"`
	AgentID string `json:"agent_id" jsonschema:"required,human caller identity, human:<name>"`
}

func registerRunCard(server *mcp.Server, svc *service.CardService, slot *RunnerSlot) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "run_card",
		Description: "Start an autonomous (Run Auto) run of a card on the agent backend, " +
			"through the same trigger path and guards as the board's run button. " +
			"Human-only: agent_id must start with \"human:\". " +
			"A non-autonomous todo card is set autonomous before the run starts; " +
			"HITL runs start only from the board. Returns at once with worker_status queued; " +
			"follow progress in the card's worker_status, phase and activity log.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input runCardInput) (*mcp.CallToolResult, *CardSummary, error) {
		if err := requireHumanAgent(input.AgentID, "run_card"); err != nil {
			return nil, nil, err
		}

		project, err := resolveProject(ctx, svc, input.Project, input.CardID)
		if err != nil {
			return nil, nil, err
		}

		run := slot.Get()
		if run == nil {
			return nil, nil, errors.New("no execution backend is configured")
		}

		card, err := svc.GetCard(ctx, project, input.CardID)
		if err != nil {
			return nil, nil, fmt.Errorf("run_card %s: %w", input.CardID, err)
		}

		// The flag must be set before the trigger so the run, the agent and the UI agree on the mode.
		// A later launch refusal (playbook lock, credential or webhook) still leaves the card
		// autonomous with no run; the board's run then forces interactive off for it.
		// Launch always refuses a run in flight (possibly HITL) or an unvetted import; those stay as they are.
		runInFlight := card.WorkerStatus == "queued" || card.WorkerStatus == "running"
		if card.State == board.StateTodo && !card.Autonomous && !runInFlight && !card.NeedsVetting() {
			yes := true
			if _, err := svc.PatchCard(ctx, project, input.CardID, service.PatchCardInput{Autonomous: &yes, AgentID: input.AgentID}); err != nil {
				return nil, nil, fmt.Errorf("run_card %s: set autonomous: %w", input.CardID, err)
			}
		}

		started, err := run(ctx, project, input.CardID)
		if err != nil {
			return nil, nil, fmt.Errorf("run_card %s: %w", input.CardID, err)
		}

		return nil, summarizeCardForAgent(started, input.AgentID), nil
	})
}

// PlaybookPlayer starts or resumes a runnable playbook's run.
type PlaybookPlayer interface {
	Play(ctx context.Context, id, agentID string) (*service.PlaybookDetail, error)
}

// --- run_playbook tool ---

type runPlaybookInput struct {
	ID      string `json:"id" jsonschema:"required,playbook id"`
	AgentID string `json:"agent_id" jsonschema:"required,human caller identity, human:<name>"`
}

func registerRunPlaybook(server *mcp.Server, pb *service.PlaybookService, player PlaybookPlayer) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "run_playbook",
		Description: "Run a playbook: make it runnable if it is not, then play it, " +
			"through the same service calls and guards as the playbook page's controls. " +
			"Human-only: agent_id must start with \"human:\". " +
			"Making it runnable forces autonomous, create_pr, await_ci and merge_pr on every card and locks them, " +
			"and each card's pull request merges into the playbook branch once CI passes; a Play refused after that " +
			"leaves the playbook runnable. A running or waiting run is refused; a run waiting on a failed card " +
			"needs a human to Stop and Play it from the board. Follow the run with get_playbook.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input runPlaybookInput) (*mcp.CallToolResult, service.PlaybookSummary, error) {
		if err := requireHumanAgent(input.AgentID, "run_playbook"); err != nil {
			return nil, service.PlaybookSummary{}, err
		}

		detail, err := pb.Get(ctx, input.ID)
		if err != nil {
			return nil, service.PlaybookSummary{}, fmt.Errorf("run_playbook %s: %w", input.ID, err)
		}

		// An already-runnable playbook skips make-runnable: the re-assert
		// costs a commit, and the runner re-forces each card before launch.
		if !detail.Runnable {
			yes := true
			if _, err := pb.UpdateMeta(ctx, input.ID, service.UpdatePlaybookInput{Runnable: &yes}, input.AgentID); err != nil {
				return nil, service.PlaybookSummary{}, fmt.Errorf("run_playbook %s: make runnable: %w", input.ID, err)
			}
		}

		played, err := player.Play(ctx, input.ID, input.AgentID)
		if err != nil {
			return nil, service.PlaybookSummary{}, fmt.Errorf("run_playbook %s: %w", input.ID, err)
		}

		return nil, redactPlaybookSummary(service.SummarizeDetail(played), input.AgentID), nil
	})
}
