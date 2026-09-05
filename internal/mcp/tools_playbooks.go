package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/service"
)

// --- Playbook tools ---
//
// All eight tools are ungated (no requireHumanAgent) - playbooks coordinate
// order for humans and planning sessions, not execution, so there is no
// permission gradient to enforce here. agent_id is declared on every tool
// (required on mutations for created_by/done_by attribution, optional on
// reads for client parity - the agent MCP client injects it into every
// call). Mutations return the slim PlaybookSummary; only get_playbook
// returns the full PlaybookDetail.

type playbookEntryToolInput struct {
	Type    string `json:"type" jsonschema:"required,entry type: card (a project card reference) or manual (a free-text gate step)"`
	Project string `json:"project,omitempty" jsonschema:"project of the referenced card (required for card entries)"`
	Card    string `json:"card,omitempty" jsonschema:"card ID (required for card entries)"`
	Text    string `json:"text,omitempty" jsonschema:"step text (required for manual entries)"`
	Note    string `json:"note,omitempty" jsonschema:"human-only commentary; never shown to executing agents"`
}

type createPlaybookInput struct {
	AgentID     string                   `json:"agent_id" jsonschema:"required,caller identity for created_by attribution"`
	Title       string                   `json:"title" jsonschema:"required,playbook title; the immutable id is derived from it"`
	Description string                   `json:"description,omitempty" jsonschema:"free-text description"`
	BoardsRepo  string                   `json:"boards_repo,omitempty" jsonschema:"boards repository to create the playbook in; defaults to the first configured repo"`
	Entries     []playbookEntryToolInput `json:"entries,omitempty" jsonschema:"initial ordered entries; the call is all-or-nothing"`
}

type listPlaybooksInput struct {
	AgentID string `json:"agent_id,omitempty" jsonschema:"caller identity (accepted for client parity)"`
}

type listPlaybooksOutput struct {
	Playbooks []service.PlaybookSummary `json:"playbooks"`
}

type getPlaybookInput struct {
	AgentID string `json:"agent_id,omitempty" jsonschema:"caller identity (accepted for client parity)"`
	ID      string `json:"id" jsonschema:"required,playbook id"`
}

type updatePlaybookInput struct {
	AgentID     string `json:"agent_id" jsonschema:"required,caller identity"`
	ID          string `json:"id" jsonschema:"required,playbook id"`
	Title       string `json:"title,omitempty" jsonschema:"new title (empty = unchanged; the id never changes)"`
	Description string `json:"description,omitempty" jsonschema:"new description (empty = unchanged)"`
}

type deletePlaybookInput struct {
	AgentID string `json:"agent_id" jsonschema:"required,caller identity"`
	ID      string `json:"id" jsonschema:"required,playbook id to delete"`
}

type deletePlaybookOutput struct {
	Deleted bool `json:"deleted"`
}

// addPlaybookEntryInput embeds playbookEntryToolInput for its five entry
// fields. Verified against the SDK's schema generator (google/jsonschema-go)
// that anonymous-embedded fields with no registered schema override are
// promoted into the parent object schema (type/project/card/text/note all
// appear as top-level properties, required propagates), so no field
// duplication is needed here.
type addPlaybookEntryInput struct {
	AgentID  string `json:"agent_id" jsonschema:"required,caller identity"`
	Playbook string `json:"playbook" jsonschema:"required,playbook id"`
	playbookEntryToolInput
}

type updatePlaybookEntryInput struct {
	AgentID  string  `json:"agent_id" jsonschema:"required,caller identity; stamps done_by on check-off"`
	Playbook string  `json:"playbook" jsonschema:"required,playbook id"`
	Entry    string  `json:"entry" jsonschema:"required,entry id (e.g. e3)"`
	Done     *bool   `json:"done,omitempty" jsonschema:"manual entries only: true checks off (stamping done_by/done_at); false unchecks and clears them"`
	Note     *string `json:"note,omitempty" jsonschema:"human-only note; applies to both entry types"`
	Text     *string `json:"text,omitempty" jsonschema:"manual entries only: replacement step text"`
	Position *int    `json:"position,omitempty" jsonschema:"move: final index after the move; clamps beyond the end; negative is invalid"`
}

type removePlaybookEntryInput struct {
	AgentID  string `json:"agent_id" jsonschema:"required,caller identity"`
	Playbook string `json:"playbook" jsonschema:"required,playbook id"`
	Entry    string `json:"entry" jsonschema:"required,entry id to remove"`
}

// nonEmptyPtr converts an empty string (the tool schema's "unchanged"
// sentinel) to a nil pointer, and a non-empty string to a pointer to it -
// matching UpdatePlaybookInput's nil-means-unchanged contract.
func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// registerPlaybookTools registers all eight playbook tools. Called only
// when the playbook subsystem is configured (registerTools checks
// cfg.Playbooks != nil).
func registerPlaybookTools(server *mcp.Server, pb *service.PlaybookService) {
	registerListPlaybooks(server, pb)
	registerGetPlaybook(server, pb)
	registerCreatePlaybook(server, pb)
	registerUpdatePlaybook(server, pb)
	registerDeletePlaybook(server, pb)
	registerAddPlaybookEntry(server, pb)
	registerUpdatePlaybookEntry(server, pb)
	registerRemovePlaybookEntry(server, pb)
}

func registerListPlaybooks(server *mcp.Server, pb *service.PlaybookService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_playbooks",
		Description: "List all playbooks with their slim list-view summary (per-entry status segments, project count, completion, manual gate indexes, next entry). Playbooks are not runnable; they coordinate order for humans and planning sessions.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listPlaybooksInput) (*mcp.CallToolResult, listPlaybooksOutput, error) {
		summaries, err := pb.List(ctx)
		if err != nil {
			return nil, listPlaybooksOutput{}, fmt.Errorf("list playbooks: %w", err)
		}

		return nil, listPlaybooksOutput{Playbooks: summaries}, nil
	})
}

func registerGetPlaybook(server *mcp.Server, pb *service.PlaybookService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_playbook",
		Description: "Get the full detail of one playbook: metadata plus every entry resolved against the card store (title, state, assigned agent for card entries; done state for manual entries).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getPlaybookInput) (*mcp.CallToolResult, *service.PlaybookDetail, error) {
		detail, err := pb.Get(ctx, input.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("get playbook %s: %w", input.ID, err)
		}

		return nil, detail, nil
	})
}

func registerCreatePlaybook(server *mcp.Server, pb *service.PlaybookService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_playbook",
		Description: "Create a cross-project playbook: an ordered list of card references and manual gate steps. Entries are validated against existing cards; the call is all-or-nothing. The playbook id is derived from the title and never changes. Playbooks are not runnable; they coordinate order for humans and planning sessions. boards_repo picks the boards repository when several are configured.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input createPlaybookInput) (*mcp.CallToolResult, service.PlaybookSummary, error) {
		entries := make([]service.PlaybookEntryInput, len(input.Entries))
		for i, e := range input.Entries {
			entries[i] = service.PlaybookEntryInput{Type: e.Type, Project: e.Project, Card: e.Card, Text: e.Text, Note: e.Note}
		}

		detail, err := pb.Create(ctx, service.CreatePlaybookInput{
			Title: input.Title, Description: input.Description, AgentID: input.AgentID, Entries: entries,
			BoardsRepo: input.BoardsRepo,
		})
		if err != nil {
			return nil, service.PlaybookSummary{}, remoteErr(fmt.Errorf("create playbook %s: %w", input.Title, err))
		}

		return nil, service.SummarizeDetail(detail), nil
	})
}

func registerUpdatePlaybook(server *mcp.Server, pb *service.PlaybookService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_playbook",
		Description: "Update a playbook's title and/or description. Empty fields are left unchanged. The playbook id is immutable - a title edit never re-slugs it.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input updatePlaybookInput) (*mcp.CallToolResult, service.PlaybookSummary, error) {
		detail, err := pb.UpdateMeta(ctx, input.ID, service.UpdatePlaybookInput{
			Title:       nonEmptyPtr(input.Title),
			Description: nonEmptyPtr(input.Description),
		}, input.AgentID)
		if err != nil {
			return nil, service.PlaybookSummary{}, fmt.Errorf("update playbook %s: %w", input.ID, err)
		}

		return nil, service.SummarizeDetail(detail), nil
	})
}

func registerDeletePlaybook(server *mcp.Server, pb *service.PlaybookService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_playbook",
		Description: "Delete a playbook. This does not affect the cards it referenced.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input deletePlaybookInput) (*mcp.CallToolResult, deletePlaybookOutput, error) {
		if err := pb.Delete(ctx, input.ID, input.AgentID); err != nil {
			return nil, deletePlaybookOutput{}, fmt.Errorf("delete playbook %s: %w", input.ID, err)
		}

		return nil, deletePlaybookOutput{Deleted: true}, nil
	})
}

func registerAddPlaybookEntry(server *mcp.Server, pb *service.PlaybookService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_playbook_entry",
		Description: "Append one new entry (card reference or manual gate step) to the end of a playbook. Card entries are validated against the card store; duplicate card references are rejected.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input addPlaybookEntryInput) (*mcp.CallToolResult, service.PlaybookSummary, error) {
		detail, err := pb.AddEntry(ctx, input.Playbook, service.PlaybookEntryInput{
			Type: input.Type, Project: input.Project, Card: input.Card, Text: input.Text, Note: input.Note,
		}, input.AgentID)
		if err != nil {
			return nil, service.PlaybookSummary{}, fmt.Errorf("add playbook entry to %s: %w", input.Playbook, err)
		}

		return nil, service.SummarizeDetail(detail), nil
	})
}

func registerUpdatePlaybookEntry(server *mcp.Server, pb *service.PlaybookService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_playbook_entry",
		Description: "Patch one playbook entry's done state, note, text, or position. Done and text apply only to manual entries; note applies to both types; checking done stamps done_by/done_at from agent_id, unchecking clears both. Position is the entry's final index after the move.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input updatePlaybookEntryInput) (*mcp.CallToolResult, service.PlaybookSummary, error) {
		detail, err := pb.UpdateEntry(ctx, input.Playbook, input.Entry, service.UpdateEntryInput{
			Done: input.Done, Note: input.Note, Text: input.Text, Position: input.Position,
		}, input.AgentID)
		if err != nil {
			return nil, service.PlaybookSummary{}, fmt.Errorf("update playbook entry %s/%s: %w", input.Playbook, input.Entry, err)
		}

		return nil, service.SummarizeDetail(detail), nil
	})
}

func registerRemovePlaybookEntry(server *mcp.Server, pb *service.PlaybookService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "remove_playbook_entry",
		Description: "Remove one entry from a playbook. The entry's id is never reused.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input removePlaybookEntryInput) (*mcp.CallToolResult, service.PlaybookSummary, error) {
		detail, err := pb.RemoveEntry(ctx, input.Playbook, input.Entry, input.AgentID)
		if err != nil {
			return nil, service.PlaybookSummary{}, fmt.Errorf("remove playbook entry %s/%s: %w", input.Playbook, input.Entry, err)
		}

		return nil, service.SummarizeDetail(detail), nil
	})
}
