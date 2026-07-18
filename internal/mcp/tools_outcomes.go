package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/metrics"
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/service"
)

// OutcomeWriter is the op-store surface this tool needs.
type OutcomeWriter interface {
	RecordModelOutcomes(ctx context.Context, rows []sqlite.ModelOutcome) error
}

type outcomeRow struct {
	Model       string  `json:"model"        jsonschema:"required,OpenRouter slug of the candidate's coder model"`
	Result      string  `json:"result"       jsonschema:"required,win | loss | failed"`
	VerifyPass  bool    `json:"verify_pass"  jsonschema:"whether the candidate's verify command passed"`
	CostUSD     float64 `json:"cost_usd"     jsonschema:"candidate spend in USD"`
	NCandidates int     `json:"n_candidates" jsonschema:"required,how many candidates raced in this game (>= 2)"`
	JudgeModel  string  `json:"judge_model,omitempty" jsonschema:"judge model slug; empty for an auto-win"`
}

type reportModelOutcomeInput struct {
	CardID   string       `json:"card_id"  jsonschema:"required,parent card of the Best-of-N run"`
	Project  string       `json:"project,omitempty" jsonschema:"project name; resolved from card_id when omitted"`
	AgentID  string       `json:"agent_id" jsonschema:"required,agent ID reporting the outcomes"`
	Outcomes []outcomeRow `json:"outcomes" jsonschema:"required,one row per candidate appearance"`
}

func reportModelOutcomeHandler(svc *service.CardService, w OutcomeWriter) func(context.Context, *mcp.CallToolRequest, reportModelOutcomeInput) (*mcp.CallToolResult, map[string]any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in reportModelOutcomeInput) (*mcp.CallToolResult, map[string]any, error) {
		if in.CardID == "" || len(in.Outcomes) == 0 {
			return nil, nil, fmt.Errorf("report_model_outcome: card_id and outcomes are required")
		}

		project, err := resolveProject(ctx, svc, in.Project, in.CardID)
		if err != nil {
			return nil, nil, fmt.Errorf("report_model_outcome: %w", err)
		}

		if err := requireActiveClaim(ctx, svc, project, in.CardID, in.AgentID, "report_model_outcome"); err != nil {
			return nil, nil, err
		}

		rows := make([]sqlite.ModelOutcome, 0, len(in.Outcomes))
		for _, o := range in.Outcomes {
			rows = append(rows, sqlite.ModelOutcome{
				Project:     project,
				CardID:      in.CardID,
				Model:       o.Model,
				Role:        "coder",
				Result:      o.Result,
				VerifyPass:  o.VerifyPass,
				CostUSD:     o.CostUSD,
				NCandidates: o.NCandidates,
				JudgeModel:  o.JudgeModel,
			})
		}

		if err := w.RecordModelOutcomes(ctx, rows); err != nil {
			return nil, nil, fmt.Errorf("report_model_outcome: %w", err)
		}

		// The result enum is store-validated, so a successful write
		// guarantees bounded label values.
		for _, row := range rows {
			metrics.ModelOutcomesTotal.WithLabelValues(row.Model, row.Result).Inc()
		}

		return nil, map[string]any{"status": "recorded", "rows": len(rows)}, nil
	}
}

func registerReportModelOutcome(server *mcp.Server, svc *service.CardService, w OutcomeWriter) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "report_model_outcome",
		Description: "Record per-candidate Best-of-N outcomes (win/loss/failed) after the judge " +
			"phase so future coder-model selection can learn from head-to-head results. " +
			"Requires the caller to hold the parent card's claim.",
	}, reportModelOutcomeHandler(svc, w))
}
