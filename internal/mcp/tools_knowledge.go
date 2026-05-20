package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// --- Knowledge base read tools ---

type getKnowledgeBaseInput struct {
	Project string `json:"project" jsonschema:"required,project name"`
	Repo    string `json:"repo,omitempty" jsonschema:"optional repo name; defaults to primary"`
}

type getKnowledgeBaseOutput struct {
	Project   string                  `json:"project"`
	Repo      string                  `json:"repo"`
	Docs      map[string]string       `json:"docs"`
	Summaries map[string]string       `json:"summaries"`
	Meta      board.KnowledgeRepoMeta `json:"meta"`
}

func registerGetKnowledgeBase(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_knowledge_base",
		Description: "Returns all knowledge-base docs for a project (and optionally a specific repo) in a single call. Intended for thinking-phase skills (brainstorming, debugging, planning) to load architectural context once.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getKnowledgeBaseInput) (*mcp.CallToolResult, getKnowledgeBaseOutput, error) {
		out, err := svc.ReadKnowledgeBase(ctx, in.Project, in.Repo)
		if err != nil {
			return nil, getKnowledgeBaseOutput{}, fmt.Errorf("get knowledge base: %w", err)
		}

		// Ensure non-nil maps so the MCP output validator sees objects, not null.
		if out.Docs == nil {
			out.Docs = map[string]string{}
		}

		if out.Summaries == nil {
			out.Summaries = map[string]string{}
		}

		if out.Meta.Docs == nil {
			out.Meta.Docs = map[string]board.KnowledgeDocMeta{}
		}

		return nil, getKnowledgeBaseOutput{
			Project:   out.Project,
			Repo:      out.Repo,
			Docs:      out.Docs,
			Summaries: out.Summaries,
			Meta:      out.Meta,
		}, nil
	})
}

type readKnowledgeDocInput struct {
	Project string `json:"project" jsonschema:"required,project name"`
	Repo    string `json:"repo,omitempty" jsonschema:"optional repo name; defaults to primary"`
	Doc     string `json:"doc" jsonschema:"required,one of architecture.md/code-structure.md/api-documentation.md/glossary.md"`
}

type readKnowledgeDocOutput struct {
	Content string                 `json:"content"`
	Meta    board.KnowledgeDocMeta `json:"meta"`
}

func registerReadKnowledgeDoc(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_knowledge_doc",
		Description: "Read a single knowledge-base doc (architecture.md, code-structure.md, api-documentation.md, glossary.md) for a project and repo.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in readKnowledgeDocInput) (*mcp.CallToolResult, readKnowledgeDocOutput, error) {
		out, err := svc.ReadKnowledgeDoc(ctx, in.Project, in.Repo, in.Doc)
		if err != nil {
			return nil, readKnowledgeDocOutput{}, fmt.Errorf("read knowledge doc: %w", err)
		}

		return nil, readKnowledgeDocOutput{Content: out.Content, Meta: out.Meta}, nil
	})
}

type listKnowledgeBasesInput struct {
	Project string `json:"project,omitempty" jsonschema:"optional project filter"`
}

type listKnowledgeBasesOutput struct {
	Bases []service.KnowledgeBaseSummary `json:"bases"`
}

func registerListKnowledgeBases(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_knowledge_bases",
		Description: "Enumerate knowledge bases across all projects (or a specific project). Returns project name, repos, and per-doc human-edited flags.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listKnowledgeBasesInput) (*mcp.CallToolResult, listKnowledgeBasesOutput, error) {
		bases, err := svc.ListKnowledgeBases(ctx, in.Project)
		if err != nil {
			return nil, listKnowledgeBasesOutput{}, fmt.Errorf("list knowledge bases: %w", err)
		}

		return nil, listKnowledgeBasesOutput{Bases: bases}, nil
	})
}

type refreshKnowledgeBaseInput struct {
	Project string `json:"project" jsonschema:"required"`
	Repo    string `json:"repo,omitempty" jsonschema:"optional repo filter"`
	AgentID string `json:"agent_id" jsonschema:"required, must start with 'human:'"`
}

type refreshKnowledgeBaseOutput struct {
	Plan service.RefreshPlan `json:"plan"`
}

func registerRefreshKnowledgeBase(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "refresh_knowledge_base",
		Description: "Human-only. Returns a build plan describing which KB docs will be rebuilt, with cost estimates and human_edited flags. Does not run sub-agents — the refresh skill spawns those and calls commit_knowledge_docs.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in refreshKnowledgeBaseInput) (*mcp.CallToolResult, refreshKnowledgeBaseOutput, error) {
		if err := requireHumanAgent(in.AgentID, "refresh_knowledge_base"); err != nil {
			return nil, refreshKnowledgeBaseOutput{}, err
		}

		plan, err := svc.BuildRefreshPlan(ctx, in.Project, in.Repo)
		if err != nil {
			return nil, refreshKnowledgeBaseOutput{}, err
		}

		return nil, refreshKnowledgeBaseOutput{Plan: *plan}, nil
	})
}

type commitKnowledgeDocsInput struct {
	Project    string            `json:"project" jsonschema:"required"`
	Repo       string            `json:"repo" jsonschema:"required"`
	HeadCommit string            `json:"head_commit" jsonschema:"required, target repo HEAD SHA at refresh time"`
	Docs       map[string]string `json:"docs" jsonschema:"required, map of doc filename to whole markdown content"`
	AgentID    string            `json:"agent_id" jsonschema:"required, must start with 'human:'"`
}

type commitKnowledgeDocsOutput struct {
	FilesWritten []string `json:"files_written"`
}

func registerCommitKnowledgeDocs(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "commit_knowledge_docs",
		Description: "Human-only. Writes refresh-produced KB docs atomically and commits them with a single message. Clears human_edited flag on each written doc.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in commitKnowledgeDocsInput) (*mcp.CallToolResult, commitKnowledgeDocsOutput, error) {
		if err := requireHumanAgent(in.AgentID, "commit_knowledge_docs"); err != nil {
			return nil, commitKnowledgeDocsOutput{}, err
		}

		res, err := svc.WriteKnowledgeDocs(ctx, service.WriteKnowledgeDocsInput{
			Project:    in.Project,
			Repo:       in.Repo,
			Docs:       in.Docs,
			HeadCommit: in.HeadCommit,
			Source:     service.KnowledgeWriteSourceRefresh,
			AgentID:    in.AgentID,
		})
		if err != nil {
			return nil, commitKnowledgeDocsOutput{}, err
		}

		return nil, commitKnowledgeDocsOutput{FilesWritten: res.FilesWritten}, nil
	})
}

type updateRefreshProgressInput struct {
	Project    string `json:"project"     jsonschema:"required"`
	Repo       string `json:"repo"        jsonschema:"required"`
	AgentID    string `json:"agent_id"    jsonschema:"required, must start with 'human:'"`
	DocsTotal  int    `json:"docs_total"  jsonschema:"required"`
	DocsDone   int    `json:"docs_done"   jsonschema:"required"`
	CurrentDoc string `json:"current_doc" jsonschema:"required"`
}

type updateRefreshProgressOutput struct {
	OK      bool `json:"ok"`
	Tracked bool `json:"tracked"`
}

func registerUpdateRefreshProgress(server *mcp.Server, svc *service.CardService) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "update_refresh_progress",
		Description: "Human-only. Reports per-doc progress from a refresh-knowledge skill " +
			"running inside the runner container. Returns tracked=false when no in-flight " +
			"job matches (project, repo) — local-mode skill calls are no-ops.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in updateRefreshProgressInput) (*mcp.CallToolResult, updateRefreshProgressOutput, error) {
		if err := requireHumanAgent(in.AgentID, "update_refresh_progress"); err != nil {
			return nil, updateRefreshProgressOutput{}, err
		}

		reg := svc.RefreshRegistry()
		if reg == nil {
			return nil, updateRefreshProgressOutput{OK: true, Tracked: false}, nil
		}

		tracked, err := reg.UpdateProgress(in.Project, in.Repo, in.DocsTotal, in.DocsDone, in.CurrentDoc)
		if err != nil {
			return nil, updateRefreshProgressOutput{}, err
		}

		return nil, updateRefreshProgressOutput{OK: true, Tracked: tracked}, nil
	})
}
