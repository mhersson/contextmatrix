package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// --- Playbook tools ---
//
// The eleven tools are ungated (no requireHumanAgent) except
// add_playbook_entry adding a card entry to a runnable playbook, which is
// human-only: a runnable playbook executes its cards. Otherwise playbooks
// coordinate order, not execution. agent_id is declared on every tool
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

type playbookCardRefInput struct {
	Project string `json:"project" jsonschema:"required,project of the card"`
	Card    string `json:"card" jsonschema:"required,card ID"`
}

type playbookFollowupToolInput struct {
	Text  string                 `json:"text" jsonschema:"required,one-line check or setup step a person does after the run"`
	Cards []playbookCardRefInput `json:"cards,omitempty" jsonschema:"cards the follow-up relates to (labels only)"`
}

type createPlaybookInput struct {
	AgentID     string                      `json:"agent_id" jsonschema:"required,caller identity for created_by attribution"`
	Title       string                      `json:"title" jsonschema:"required,playbook title; the immutable id is derived from it"`
	Description string                      `json:"description,omitempty" jsonschema:"free-text description"`
	BoardsRepo  string                      `json:"boards_repo,omitempty" jsonschema:"boards repository to create the playbook in; defaults to the first configured repo"`
	Entries     []playbookEntryToolInput    `json:"entries,omitempty" jsonschema:"initial ordered entries; the call is all-or-nothing"`
	Followups   []playbookFollowupToolInput `json:"followups,omitempty" jsonschema:"after-run checks for a person, never run as steps; the call is all-or-nothing"`
}

type listPlaybooksInput struct {
	AgentID string `json:"agent_id,omitempty" jsonschema:"caller identity - unvetted external card titles are redacted for non-human callers"`
}

type listPlaybooksOutput struct {
	Playbooks []service.PlaybookSummary `json:"playbooks"`
}

type getPlaybookInput struct {
	AgentID string `json:"agent_id,omitempty" jsonschema:"caller identity - unvetted external card titles are redacted for non-human callers"`
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

type addPlaybookFollowupInput struct {
	AgentID  string                 `json:"agent_id" jsonschema:"required,caller identity"`
	Playbook string                 `json:"playbook" jsonschema:"required,playbook id"`
	Text     string                 `json:"text" jsonschema:"required,one-line check or setup step a person does after the run"`
	Cards    []playbookCardRefInput `json:"cards,omitempty" jsonschema:"cards the follow-up relates to (labels only)"`
	Position *int                   `json:"position,omitempty" jsonschema:"insert at this index; omitted appends; clamps beyond the end; negative is invalid"`
}

type updatePlaybookFollowupInput struct {
	AgentID  string                 `json:"agent_id" jsonschema:"required,caller identity; stamps done_by on check-off"`
	Playbook string                 `json:"playbook" jsonschema:"required,playbook id"`
	Followup string                 `json:"followup" jsonschema:"required,follow-up id (e.g. f2)"`
	Done     *bool                  `json:"done,omitempty" jsonschema:"true checks off (stamping done_by/done_at); false unchecks and clears them"`
	Text     *string                `json:"text,omitempty" jsonschema:"replacement text"`
	Cards    []playbookCardRefInput `json:"cards,omitempty" jsonschema:"replaces the related cards; omitted leaves them unchanged, [] clears them"`
	Position *int                   `json:"position,omitempty" jsonschema:"move: final index after the move; clamps beyond the end; negative is invalid"`
}

type removePlaybookFollowupInput struct {
	AgentID  string `json:"agent_id" jsonschema:"required,caller identity"`
	Playbook string `json:"playbook" jsonschema:"required,playbook id"`
	Followup string `json:"followup" jsonschema:"required,follow-up id to remove"`
}

func cardRefsFromInput(in []playbookCardRefInput) []board.PlaybookCardRef {
	if in == nil {
		return nil
	}

	refs := make([]board.PlaybookCardRef, len(in))
	for i, c := range in {
		refs[i] = board.PlaybookCardRef{Project: c.Project, Card: c.Card}
	}

	return refs
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

// registerPlaybookTools registers all eleven playbook tools. Called only
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
	registerAddPlaybookFollowup(server, pb)
	registerUpdatePlaybookFollowup(server, pb)
	registerRemovePlaybookFollowup(server, pb)
}

func registerListPlaybooks(server *mcp.Server, pb *service.PlaybookService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_playbooks",
		Description: "List all playbooks with their slim list-view summary (per-entry status segments, project count, completion, manual gate indexes, next entry). A runnable playbook (runnable: true) runs its cards in order on a shared playbook branch; the run_playbook tool and the web UI start it; stop and base_branch are human-only web UI controls.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input listPlaybooksInput) (*mcp.CallToolResult, listPlaybooksOutput, error) {
		summaries, err := pb.List(ctx)
		if err != nil {
			return nil, listPlaybooksOutput{}, fmt.Errorf("list playbooks: %w", err)
		}

		out := make([]service.PlaybookSummary, len(summaries))
		for i, sm := range summaries {
			out[i] = redactPlaybookSummary(sm, input.AgentID)
		}

		return nil, listPlaybooksOutput{Playbooks: out}, nil
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

		return nil, redactPlaybookDetail(detail, input.AgentID), nil
	})
}

func registerCreatePlaybook(server *mcp.Server, pb *service.PlaybookService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_playbook",
		Description: "Create a cross-project playbook: an ordered list of card references and manual gate steps. Entries are validated against existing cards; the call is all-or-nothing. Optional followups are checks a person makes after the run (never run as steps); they are validated with the entries, so one bad entry or follow-up creates nothing. The playbook id is derived from the title and never changes. Playbooks are created not runnable; run_playbook or a human in the web UI makes one runnable. boards_repo picks the boards repository when several are configured.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input createPlaybookInput) (*mcp.CallToolResult, service.PlaybookSummary, error) {
		entries := make([]service.PlaybookEntryInput, len(input.Entries))
		for i, e := range input.Entries {
			entries[i] = service.PlaybookEntryInput{Type: e.Type, Project: e.Project, Card: e.Card, Text: e.Text, Note: e.Note}
		}

		followups := make([]service.PlaybookFollowupInput, len(input.Followups))
		for i, f := range input.Followups {
			followups[i] = service.PlaybookFollowupInput{Text: f.Text, Cards: cardRefsFromInput(f.Cards)}
		}

		detail, err := pb.Create(ctx, service.CreatePlaybookInput{
			Title: input.Title, Description: input.Description, AgentID: input.AgentID, Entries: entries,
			Followups:  followups,
			BoardsRepo: input.BoardsRepo,
		})
		if err != nil {
			return nil, service.PlaybookSummary{}, remoteErr(fmt.Errorf("create playbook %s: %w", input.Title, err))
		}

		return nil, redactPlaybookSummary(service.SummarizeDetail(detail), input.AgentID), nil
	})
}

func registerUpdatePlaybook(server *mcp.Server, pb *service.PlaybookService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_playbook",
		Description: "Update a playbook's title and/or description. Empty fields are left unchanged. The playbook id is immutable - a title edit never re-slugs it. runnable, base_branch and run state are operator fields this tool preserves; run_playbook makes a playbook runnable and runs it.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input updatePlaybookInput) (*mcp.CallToolResult, service.PlaybookSummary, error) {
		detail, err := pb.UpdateMeta(ctx, input.ID, service.UpdatePlaybookInput{
			Title:       nonEmptyPtr(input.Title),
			Description: nonEmptyPtr(input.Description),
		}, input.AgentID)
		if err != nil {
			return nil, service.PlaybookSummary{}, fmt.Errorf("update playbook %s: %w", input.ID, err)
		}

		return nil, redactPlaybookSummary(service.SummarizeDetail(detail), input.AgentID), nil
	})
}

func registerDeletePlaybook(server *mcp.Server, pb *service.PlaybookService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_playbook",
		Description: "Delete a playbook. This does not affect the cards it referenced. Refused while a run is running or waiting.",
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
		Description: "Append one new entry (card reference or manual gate step) to the end of a playbook. Card entries are validated against the card store; duplicate card references are rejected. Adding a card entry to a runnable playbook is human-only.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input addPlaybookEntryInput) (*mcp.CallToolResult, service.PlaybookSummary, error) {
		if input.Type == board.EntryTypeCard {
			detail, err := pb.Get(ctx, input.Playbook)
			if err != nil {
				return nil, service.PlaybookSummary{}, fmt.Errorf("add playbook entry to %s: %w", input.Playbook, err)
			}

			if detail.Runnable {
				if err := requireHumanAgent(input.AgentID, "add_playbook_entry on a runnable playbook"); err != nil {
					return nil, service.PlaybookSummary{}, err
				}
			}
		}

		detail, err := pb.AddEntry(ctx, input.Playbook, service.PlaybookEntryInput{
			Type: input.Type, Project: input.Project, Card: input.Card, Text: input.Text, Note: input.Note,
		}, input.AgentID)
		if err != nil {
			return nil, service.PlaybookSummary{}, fmt.Errorf("add playbook entry to %s: %w", input.Playbook, err)
		}

		return nil, redactPlaybookSummary(service.SummarizeDetail(detail), input.AgentID), nil
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

		return nil, redactPlaybookSummary(service.SummarizeDetail(detail), input.AgentID), nil
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

		return nil, redactPlaybookSummary(service.SummarizeDetail(detail), input.AgentID), nil
	})
}

func registerAddPlaybookFollowup(server *mcp.Server, pb *service.PlaybookService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_playbook_followup",
		Description: "Add a follow-up to a playbook: a check or setup step a person does after the run. Follow-ups are never run as steps and never count toward progress. Appends, or inserts at position. Not human-gated and allowed during a run; it never changes run state.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input addPlaybookFollowupInput) (*mcp.CallToolResult, service.PlaybookSummary, error) {
		detail, err := pb.AddFollowup(ctx, input.Playbook, service.PlaybookFollowupInput{
			Text: input.Text, Cards: cardRefsFromInput(input.Cards), Position: input.Position,
		}, input.AgentID)
		if err != nil {
			return nil, service.PlaybookSummary{}, fmt.Errorf("add playbook follow-up to %s: %w", input.Playbook, err)
		}

		return nil, redactPlaybookSummary(service.SummarizeDetail(detail), input.AgentID), nil
	})
}

func registerUpdatePlaybookFollowup(server *mcp.Server, pb *service.PlaybookService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_playbook_followup",
		Description: "Patch one playbook follow-up's done state, text, related cards, or position. Checking done stamps done_by/done_at from agent_id, unchecking clears both; cards replaces the list ([] clears it). Not human-gated and allowed during a run; it never changes run state.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input updatePlaybookFollowupInput) (*mcp.CallToolResult, service.PlaybookSummary, error) {
		patch := service.UpdateFollowupInput{Done: input.Done, Text: input.Text, Position: input.Position}
		if input.Cards != nil {
			refs := cardRefsFromInput(input.Cards)
			patch.Cards = &refs
		}

		detail, err := pb.UpdateFollowup(ctx, input.Playbook, input.Followup, patch, input.AgentID)
		if err != nil {
			return nil, service.PlaybookSummary{}, fmt.Errorf("update playbook follow-up %s/%s: %w", input.Playbook, input.Followup, err)
		}

		return nil, redactPlaybookSummary(service.SummarizeDetail(detail), input.AgentID), nil
	})
}

func registerRemovePlaybookFollowup(server *mcp.Server, pb *service.PlaybookService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "remove_playbook_followup",
		Description: "Remove one follow-up from a playbook. The follow-up's id is never reused. Not human-gated and allowed during a run; it never changes run state.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input removePlaybookFollowupInput) (*mcp.CallToolResult, service.PlaybookSummary, error) {
		detail, err := pb.RemoveFollowup(ctx, input.Playbook, input.Followup, input.AgentID)
		if err != nil {
			return nil, service.PlaybookSummary{}, fmt.Errorf("remove playbook follow-up %s/%s: %w", input.Playbook, input.Followup, err)
		}

		return nil, redactPlaybookSummary(service.SummarizeDetail(detail), input.AgentID), nil
	})
}
