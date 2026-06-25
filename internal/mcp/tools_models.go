package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// BlacklistWriter is the op-store surface this tool needs.
type BlacklistWriter interface {
	RecordIncapableModel(ctx context.Context, slug, reason, sampleCard, reportedBy string) error
}

type reportIncapableModelInput struct {
	ModelSlug    string `json:"model_slug"    jsonschema:"required,OpenRouter slug that could not drive the tool loop"`
	Reason       string `json:"reason"        jsonschema:"required,why the model was incapable (parse failures / no-progress)"`
	SampleCardID string `json:"sample_card_id,omitempty" jsonschema:"card where the incapability was observed"`
	AgentID      string `json:"agent_id"      jsonschema:"required,agent ID reporting the incapability"`
}

func reportIncapableModelHandler(w BlacklistWriter) func(context.Context, *mcp.CallToolRequest, reportIncapableModelInput) (*mcp.CallToolResult, map[string]any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in reportIncapableModelInput) (*mcp.CallToolResult, map[string]any, error) {
		if in.ModelSlug == "" || in.Reason == "" {
			return nil, nil, fmt.Errorf("report_incapable_model: model_slug and reason are required")
		}

		if err := w.RecordIncapableModel(ctx, in.ModelSlug, in.Reason, in.SampleCardID, in.AgentID); err != nil {
			return nil, nil, fmt.Errorf("report_incapable_model: %w", err)
		}

		return nil, map[string]any{"status": "recorded", "slug": in.ModelSlug}, nil
	}
}

func registerReportIncapableModel(server *mcp.Server, w BlacklistWriter) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "report_incapable_model",
		Description: "Record that a model could not drive the tool loop (invalid/empty tool calls " +
			"past the repair budget, or no-progress turns) so it is never auto-selected again. " +
			"Agent-callable. Idempotent per model slug.",
	}, reportIncapableModelHandler(w))
}
