package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/knowledge"
	"github.com/mhersson/contextmatrix/internal/mcp/mcpcontext"
	"github.com/mhersson/contextmatrix/internal/storage"
)

var knowledgeToolNames = []string{"get_knowledge_context", "query_knowledge", "get_knowledge_item"}

type fakeKnowledge struct {
	mu      sync.Mutex
	calls   int
	method  string
	target  knowledge.Target
	text    string
	kinds   []string
	limit   int
	ids     []string
	related bool

	answer knowledge.Answer
	err    error
}

func (f *fakeKnowledge) record(method string, t knowledge.Target) (knowledge.Answer, error) {
	f.calls++
	f.method = method
	f.target = t

	return f.answer, f.err
}

func (f *fakeKnowledge) Context(_ context.Context, t knowledge.Target, task string, limit int) (knowledge.Answer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.text, f.limit = task, limit

	return f.record("context", t)
}

func (f *fakeKnowledge) Query(_ context.Context, t knowledge.Target, query string, kinds []string, limit int) (knowledge.Answer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.text, f.kinds, f.limit = query, kinds, limit

	return f.record("query", t)
}

func (f *fakeKnowledge) Item(_ context.Context, t knowledge.Target, ids []string, related bool) (knowledge.Answer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.ids, f.related = ids, related

	return f.record("item", t)
}

func (f *fakeKnowledge) set(a knowledge.Answer, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.answer, f.err = a, err
}

func (f *fakeKnowledge) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

// setupKnowledge serves the knowledge tools from a fake and gives
// test-project provider "intent" with scope "payments".
func setupKnowledge(t *testing.T) (*testEnv, *fakeKnowledge) {
	t.Helper()

	fake := &fakeKnowledge{answer: knowledge.Answer{"items": []any{}}}
	env := setupMCP(t, func(c *ServerConfig) { c.Knowledge = fake })

	seed := pocSeedInput(nil)
	seed.KnowledgeProvider = new("intent")
	seed.KnowledgeScope = new("payments")

	_, err := env.svc.UpdateProject(context.Background(), "test-project", seed)
	require.NoError(t, err)

	return env, fake
}

// knowledgeResult checks the call succeeded and that the text block carries
// the same JSON as the structured content, then returns the latter.
func knowledgeResult(t *testing.T, res *mcp.CallToolResult, err error) map[string]any {
	t.Helper()

	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", resultText(res))

	structured, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok, "structured content is %T", res.StructuredContent)

	var text map[string]any
	require.NoError(t, json.Unmarshal([]byte(resultText(res)), &text))
	assert.Equal(t, structured, text)

	return structured
}

func knowledgeArgs(tool string) map[string]any {
	switch tool {
	case "get_knowledge_context":
		return map[string]any{"project": "test-project", "task": "refund flow"}
	case "query_knowledge":
		return map[string]any{"project": "test-project", "query": "refunds"}
	default:
		return map[string]any{"project": "test-project", "ids": []string{"R-1"}}
	}
}

func TestKnowledgeTools_RegisteredOnlyWithReader(t *testing.T) {
	ctx := context.Background()

	plain, err := setupMCP(t).session.ListTools(ctx, nil)
	require.NoError(t, err)

	for _, tool := range plain.Tools {
		assert.NotContains(t, knowledgeToolNames, tool.Name)
	}

	env, _ := setupKnowledge(t)

	served, err := env.session.ListTools(ctx, nil)
	require.NoError(t, err)

	byName := make(map[string]*mcp.Tool, len(served.Tools))
	for _, tool := range served.Tools {
		byName[tool.Name] = tool
	}

	for _, name := range knowledgeToolNames {
		tool, ok := byName[name]
		require.True(t, ok, "%s not registered", name)
		assert.NotNil(t, tool.OutputSchema, "%s has no output schema", name)
	}
}

func TestKnowledgeContext_UsesProjectProviderAndScope(t *testing.T) {
	env, fake := setupKnowledge(t)

	items := []any{
		map[string]any{"id": "R-1", "kind": "rule", "title": "Refunds", "text": "Refund within 30 days.", "agreed": true},
		map[string]any{"id": "D-2", "kind": "decision", "title": "Partial refunds", "text": "Open."},
	}
	fake.set(knowledge.Answer{"items": items, "note": "two found"}, nil)

	res, err := callToolRaw(t, env, "get_knowledge_context", map[string]any{
		"project": "test-project",
		"task":    "refund flow",
		"limit":   5,
		"caller":  "blueprint B1 stage intake",
	})
	got := knowledgeResult(t, res, err)

	assert.Equal(t, "context", fake.method)
	assert.Equal(t, knowledge.Target{Provider: "intent", Scope: "payments", Project: "test-project", Caller: "blueprint B1 stage intake"}, fake.target)
	assert.Equal(t, "refund flow", fake.text)
	assert.Equal(t, 5, fake.limit)

	assert.Equal(t, "knowledge:intent", got["source"])
	assert.Equal(t, items, got["items"])
	assert.Equal(t, "two found", got["note"])
	assert.NotContains(t, got, "reason")
}

func TestKnowledgeTools_CardIDResolvesProject(t *testing.T) {
	env, fake := setupKnowledge(t)
	card := createTestCard(t, env, "Refunds", "task", "medium")

	res, err := callToolRaw(t, env, "query_knowledge", map[string]any{
		"card_id": card.ID,
		"query":   "refunds",
		"kinds":   []string{"rule", "decision"},
	})
	got := knowledgeResult(t, res, err)

	assert.Equal(t, "query", fake.method)
	assert.Equal(t, "test-project", fake.target.Project)
	assert.Equal(t, "intent", fake.target.Provider)
	assert.Equal(t, "payments", fake.target.Scope)
	assert.Equal(t, "refunds", fake.text)
	assert.Equal(t, []string{"rule", "decision"}, fake.kinds)
	assert.Equal(t, "knowledge:intent", got["source"])
}

func TestKnowledgeItem_TrimsIDsAndPassesRelated(t *testing.T) {
	env, fake := setupKnowledge(t)

	res, err := callToolRaw(t, env, "get_knowledge_item", map[string]any{
		"project": "test-project",
		"ids":     []string{" R-1 ", "", "G-2"},
		"related": true,
	})
	knowledgeResult(t, res, err)

	assert.Equal(t, "item", fake.method)
	assert.Equal(t, []string{"R-1", "G-2"}, fake.ids)
	assert.True(t, fake.related)
}

func TestKnowledgeTools_InputErrors(t *testing.T) {
	env, fake := setupKnowledge(t)

	tests := []struct {
		name, tool string
		args       map[string]any
	}{
		{"neither project nor card", "get_knowledge_context", map[string]any{"task": "t"}},
		{"unknown project", "get_knowledge_context", map[string]any{"project": "nope", "task": "t"}},
		{"unknown card", "query_knowledge", map[string]any{"card_id": "TEST-999", "query": "q"}},
		{"blank task", "get_knowledge_context", map[string]any{"project": "test-project", "task": "  "}},
		{"blank query", "query_knowledge", map[string]any{"project": "test-project", "query": ""}},
		{"no ids", "get_knowledge_item", map[string]any{"project": "test-project", "ids": []string{}}},
		{"only blank ids", "get_knowledge_item", map[string]any{"project": "test-project", "ids": []string{" "}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, resultIsError(callToolRaw(t, env, tt.tool, tt.args)))
		})
	}

	assert.Zero(t, fake.callCount(), "no input error may reach the provider")
}

func TestKnowledgeTools_NoProviderOrScope(t *testing.T) {
	fake := &fakeKnowledge{}
	env := setupMCP(t, func(c *ServerConfig) { c.Knowledge = fake })

	check := func(t *testing.T) {
		t.Helper()

		for _, tool := range knowledgeToolNames {
			res, err := callToolRaw(t, env, tool, knowledgeArgs(tool))
			got := knowledgeResult(t, res, err)

			assert.Equal(t, "knowledge", got["source"], tool)
			assert.Equal(t, []any{}, got["items"], tool)
			assert.Contains(t, resultText(res), `"items":[]`, tool)
			assert.NotEmpty(t, got["reason"], tool)
		}
	}

	t.Run("no provider", check)

	t.Run("provider without scope", func(t *testing.T) {
		cfg, err := env.svc.GetProject(context.Background(), "test-project")
		require.NoError(t, err)

		cfg.KnowledgeProvider = "intent"
		cfg.KnowledgeScope = ""
		require.NoError(t, board.SaveProjectConfig(filepath.Join(env.boardsDir, "test-project"), cfg))
		fs, ok := env.store.(*storage.FilesystemStore)
		require.True(t, ok)
		require.NoError(t, fs.ReloadIndex(context.Background()))

		check(t)
	})

	assert.Zero(t, fake.callCount(), "a project without a usable provider never reaches the reader")
}

func TestKnowledgeTools_ErrorsBecomeReasons(t *testing.T) {
	env, fake := setupKnowledge(t)

	tests := []struct {
		err    error
		source string
	}{
		{knowledge.ErrUnknownProvider, "knowledge"},
		{knowledge.ErrProviderAbsent, "knowledge"},
		{knowledge.ErrTimeout, "knowledge:intent"},
		{knowledge.ErrProviderFailed, "knowledge:intent"},
	}

	for _, tool := range knowledgeToolNames {
		t.Run(tool, func(t *testing.T) {
			reasons := make(map[string]bool, len(tests))

			for _, tt := range tests {
				fake.set(nil, fmt.Errorf("%w: intent: provider said LEAKMARK", tt.err))

				res, err := callToolRaw(t, env, tool, knowledgeArgs(tool))
				got := knowledgeResult(t, res, err)

				assert.Equal(t, tt.source, got["source"], tt.err)
				assert.Equal(t, []any{}, got["items"], tt.err)

				reason, _ := got["reason"].(string)
				assert.NotEmpty(t, reason, tt.err)
				assert.NotContains(t, resultText(res), "LEAKMARK", "error text must not reach the result")

				reasons[reason] = true
			}

			assert.Len(t, reasons, len(tests), "each failure kind has its own reason")
		})
	}
}

func TestKnowledgeTools_PassThrough(t *testing.T) {
	env, fake := setupKnowledge(t)

	answer := knowledge.Answer{
		"items":   []any{map[string]any{"id": "R-1", "kind": "rule", "title": "t", "text": "x", "x_extra": "kept"}},
		"related": []any{map[string]any{"id": "G-1", "kind": "goal", "title": "g", "text": "y"}},
		"missing": []any{"R-9"},
		"x_top":   json.Number("1"),
		"source":  "evil",
		"reason":  "x",
	}
	fake.set(answer, nil)

	res, err := callToolRaw(t, env, "get_knowledge_item", map[string]any{"project": "test-project", "ids": []string{"R-1", "R-9"}, "related": true})
	got := knowledgeResult(t, res, err)

	assert.Equal(t, answer["items"], got["items"])
	assert.Equal(t, answer["related"], got["related"])
	assert.Equal(t, answer["missing"], got["missing"])
	assert.EqualValues(t, 1, got["x_top"])
	assert.Equal(t, "knowledge:intent", got["source"])
	assert.NotContains(t, got, "reason", "a provider-sent reason is dropped")

	assert.Equal(t, "evil", answer["source"], "the reader's answer is not modified")
	assert.Equal(t, "x", answer["reason"], "the reader's answer is not modified")
}

func TestKnowledgeTools_AnswerWithoutItems(t *testing.T) {
	env, fake := setupKnowledge(t)
	fake.set(knowledge.Answer{"note": "nothing"}, nil)

	res, err := callToolRaw(t, env, "get_knowledge_context", knowledgeArgs("get_knowledge_context"))
	got := knowledgeResult(t, res, err)

	assert.Equal(t, []any{}, got["items"])
	assert.Equal(t, "knowledge:intent", got["source"])
}

func TestKnowledgeTools_LimitClamp(t *testing.T) {
	env, fake := setupKnowledge(t)

	tests := []struct {
		tool       string
		limit, got int
	}{
		{"get_knowledge_context", 500, 50},
		{"get_knowledge_context", -3, 0},
		{"get_knowledge_context", 7, 7},
		{"query_knowledge", 500, 50},
		{"query_knowledge", 0, 0},
	}

	for _, tt := range tests {
		args := knowledgeArgs(tt.tool)
		args["limit"] = tt.limit

		res, err := callToolRaw(t, env, tt.tool, args)
		knowledgeResult(t, res, err)

		assert.Equal(t, tt.got, fake.limit, "%s limit %d", tt.tool, tt.limit)
	}
}

func TestKnowledgeTools_AgentIDFillsCaller(t *testing.T) {
	env, fake := setupKnowledge(t)

	args := knowledgeArgs("query_knowledge")
	args["agent_id"] = "agent-7"

	res, err := callToolRaw(t, env, "query_knowledge", args)
	knowledgeResult(t, res, err)

	assert.Equal(t, "agent-7", fake.target.Caller)
}

func TestKnowledgeCaller(t *testing.T) {
	const session = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"

	long := strings.Repeat("é", 300)

	tests := []struct {
		name            string
		inChat          bool
		caller, agentID string
		want            string
	}{
		{"caller wins", true, "  blueprint B1 stage intake ", "agent-1", "blueprint B1 stage intake"},
		{"chat session", true, "", "agent-1", "chat " + session},
		{"blank caller falls through", true, "   ", "", "chat " + session},
		{"agent id", false, "", "agent-1", "agent-1"},
		{"nothing", false, "", "", ""},
		{"long caller", false, long, "", strings.Repeat("é", 256)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.inChat {
				ctx = mcpcontext.WithChatSession(ctx, session)
			}

			got := knowledgeCaller(ctx, tt.caller, tt.agentID)
			assert.Equal(t, tt.want, got)
			assert.LessOrEqual(t, utf8.RuneCountInString(got), 256)
		})
	}
}
