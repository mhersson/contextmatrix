package mcp

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

func jsonTagName(tag string) string {
	name, _, _ := strings.Cut(tag, ",")

	return name
}

// TestCardSummaryMirrorsBoardCard is the drift guard for the parallel struct:
// every JSON-visible board.Card field must exist on CardSummary with an
// identical json tag and type, except body and activity_log, which are
// deliberately absent (they are the two unbounded fields MCP results must not
// echo). CardSummary must carry nothing board.Card does not have.
func TestCardSummaryMirrorsBoardCard(t *testing.T) {
	dropped := map[string]bool{"body": true, "activity_log": true}

	cardType := reflect.TypeFor[board.Card]()
	sumType := reflect.TypeFor[CardSummary]()

	sumFields := make(map[string]reflect.StructField, sumType.NumField())

	for f := range sumType.Fields() {
		name := jsonTagName(f.Tag.Get("json"))
		require.NotEmpty(t, name, "CardSummary field %s must carry a json tag", f.Name)
		require.NotEqual(t, "-", name, "CardSummary field %s must be JSON-visible", f.Name)
		sumFields[name] = f
	}

	matched := make(map[string]bool)

	for cf := range cardType.Fields() {
		tag := cf.Tag.Get("json")

		name := jsonTagName(tag)
		if name == "" || name == "-" {
			continue
		}

		if dropped[name] {
			_, present := sumFields[name]
			assert.False(t, present, "field %q must stay out of CardSummary", name)

			continue
		}

		sf, ok := sumFields[name]
		require.True(t, ok, "board.Card field %q missing from CardSummary - update the summary type", name)
		assert.Equal(t, tag, sf.Tag.Get("json"), "json tag mismatch for %q", name)
		assert.Equal(t, cf.Type, sf.Type, "type mismatch for %q", name)

		matched[name] = true
	}

	for name := range sumFields {
		assert.True(t, matched[name], "CardSummary field %q has no board.Card counterpart", name)
	}
}

// fillNonZero sets every exported field of v to a deterministic non-zero
// value so the copy test below stays exhaustive as board.Card grows.
func fillNonZero(v reflect.Value, seed int) {
	switch v.Kind() {
	case reflect.String:
		v.SetString(fmt.Sprintf("v%d", seed))
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(seed) + 1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(seed) + 0.5)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fillNonZero(v.Elem(), seed)
	case reflect.Slice:
		elem := reflect.New(v.Type().Elem()).Elem()
		fillNonZero(elem, seed)
		v.Set(reflect.Append(reflect.MakeSlice(v.Type(), 0, 1), elem))
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
		v.SetMapIndex(reflect.ValueOf("k"), reflect.ValueOf(fmt.Sprintf("v%d", seed)))
	case reflect.Interface:
		v.Set(reflect.ValueOf(fmt.Sprintf("v%d", seed)))
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			v.Set(reflect.ValueOf(time.Date(2026, 8, seed%28+1, 12, 0, 0, 0, time.UTC)))

			return
		}

		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				fillNonZero(v.Field(i), seed+i+1)
			}
		}
	default:
		panic(fmt.Sprintf("fillNonZero: unhandled kind %s", v.Kind()))
	}
}

func TestSummarizeCard(t *testing.T) {
	t.Run("nil card", func(t *testing.T) {
		assert.Nil(t, summarizeCard(nil))
	})

	t.Run("keeps every field except body and activity_log", func(t *testing.T) {
		card := &board.Card{}
		fillNonZero(reflect.ValueOf(card).Elem(), 1)
		require.NotEmpty(t, card.Body)
		require.NotEmpty(t, card.ActivityLog)

		full, err := json.Marshal(card)
		require.NoError(t, err)
		slim, err := json.Marshal(summarizeCard(card))
		require.NoError(t, err)

		var fullMap, slimMap map[string]any
		require.NoError(t, json.Unmarshal(full, &fullMap))
		require.NoError(t, json.Unmarshal(slim, &slimMap))

		require.Contains(t, fullMap, "body")
		delete(fullMap, "body")
		delete(fullMap, "activity_log")
		assert.Equal(t, fullMap, slimMap, "summarizeCard must copy every field except body and activity_log")
	})
}

func TestSummarizeCards(t *testing.T) {
	assert.Nil(t, summarizeCards(nil), "nil in, nil out")

	empty := summarizeCards([]*board.Card{})
	require.NotNil(t, empty, "empty slice must stay non-nil so the wire keeps an empty array")
	assert.Empty(t, empty)

	out := summarizeCards([]*board.Card{{ID: "CMX-001", Body: "spec"}, nil})
	require.Len(t, out, 2)
	assert.Equal(t, "CMX-001", out[0].ID)
	assert.Nil(t, out[1])
}

// assertSlimCardMap checks the wire contract for a summarized card: scalar
// identity fields present, the two unbounded fields absent.
func assertSlimCardMap(t *testing.T, m map[string]any) {
	t.Helper()

	assert.NotContains(t, m, "body")
	assert.NotContains(t, m, "activity_log")
	assert.Contains(t, m, "id")
	assert.Contains(t, m, "state")
}

// newLoggedCard creates a card with a non-empty body and one activity-log
// entry so a full-card echo would visibly carry both.
func newLoggedCard(t *testing.T, env *testEnv, title string) *board.Card {
	t.Helper()

	card, err := env.svc.CreateCard(t.Context(), "test-project", service.CreateCardInput{
		Title:    title,
		Type:     "task",
		Priority: "medium",
		Body:     "## Spec\nLong specification text that must never be echoed.",
	})
	require.NoError(t, err)

	_, err = env.svc.AddLogEntry(t.Context(), "test-project", card.ID, board.ActivityEntry{
		Agent:   "agent-1",
		Action:  "note",
		Message: "setup entry",
	})
	require.NoError(t, err)

	return card
}

func claimLoggedCard(t *testing.T, env *testEnv, title string) *board.Card {
	t.Helper()

	card := newLoggedCard(t, env, title)
	_, err := env.svc.ClaimCard(t.Context(), "test-project", card.ID, "agent-1")
	require.NoError(t, err)

	return card
}

// TestSlimToolResultsOmitBodyAndActivityLog pins the wire contract the
// contextmatrix-agent cmclient depends on: mutation and list tools return
// card summaries (never body or activity_log), with the scalar fields each
// consumer parses still present. get_card and get_task_context stay full.
func TestSlimToolResultsOmitBodyAndActivityLog(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		path  string // where the card JSON lives: root, card, or cards
		setup func(t *testing.T, env *testEnv) map[string]any
		extra func(t *testing.T, root map[string]any)
	}{
		{
			name: "create_card",
			tool: "create_card",
			path: "root",
			setup: func(t *testing.T, _ *testEnv) map[string]any {
				t.Helper()

				return map[string]any{
					"project": "test-project", "title": "Slim create", "type": "task",
					"priority": "medium", "body": "## Spec\nNot echoed.",
				}
			},
			extra: func(t *testing.T, root map[string]any) {
				t.Helper()
				assert.Contains(t, root, "title")
			},
		},
		{
			name: "update_card",
			tool: "update_card",
			path: "root",
			setup: func(t *testing.T, env *testEnv) map[string]any {
				t.Helper()
				card := newLoggedCard(t, env, "Slim update")

				return map[string]any{"card_id": card.ID, "body": "## Updated\nStill not echoed."}
			},
		},
		{
			name: "transition_card",
			tool: "transition_card",
			path: "root",
			setup: func(t *testing.T, env *testEnv) map[string]any {
				t.Helper()
				card := newLoggedCard(t, env, "Slim transition")

				return map[string]any{"card_id": card.ID, "new_state": "in_progress"}
			},
		},
		{
			name: "claim_card",
			tool: "claim_card",
			path: "root",
			setup: func(t *testing.T, env *testEnv) map[string]any {
				t.Helper()
				card := newLoggedCard(t, env, "Slim claim")

				return map[string]any{"card_id": card.ID, "agent_id": "agent-1"}
			},
			extra: func(t *testing.T, root map[string]any) {
				t.Helper()
				assert.Equal(t, "agent-1", root["assigned_agent"], "skills verify their claim via assigned_agent")
			},
		},
		{
			name: "release_card",
			tool: "release_card",
			path: "root",
			setup: func(t *testing.T, env *testEnv) map[string]any {
				t.Helper()
				card := claimLoggedCard(t, env, "Slim release")

				return map[string]any{"card_id": card.ID, "agent_id": "agent-1"}
			},
		},
		{
			name: "add_log",
			tool: "add_log",
			path: "root",
			setup: func(t *testing.T, env *testEnv) map[string]any {
				t.Helper()
				card := claimLoggedCard(t, env, "Slim add_log")

				return map[string]any{
					"card_id": card.ID, "agent_id": "agent-1",
					"action": "status_update", "message": "progress",
				}
			},
		},
		{
			name: "complete_task",
			tool: "complete_task",
			path: "card",
			setup: func(t *testing.T, env *testEnv) map[string]any {
				t.Helper()
				card := claimLoggedCard(t, env, "Slim complete")

				return map[string]any{"card_id": card.ID, "agent_id": "agent-1", "summary": "done"}
			},
		},
		{
			name: "report_push",
			tool: "report_push",
			path: "card",
			setup: func(t *testing.T, env *testEnv) map[string]any {
				t.Helper()
				card := claimLoggedCard(t, env, "Slim push")

				return map[string]any{"card_id": card.ID, "agent_id": "agent-1", "branch": "feat/slim"}
			},
		},
		{
			name: "report_usage",
			tool: "report_usage",
			path: "root",
			setup: func(t *testing.T, env *testEnv) map[string]any {
				t.Helper()
				card := newLoggedCard(t, env, "Slim usage")

				return map[string]any{
					"card_id": card.ID, "agent_id": "agent-1",
					"prompt_tokens": 100, "completion_tokens": 50,
				}
			},
			extra: func(t *testing.T, root map[string]any) {
				t.Helper()
				assert.Contains(t, root, "token_usage", "agent budget ledger parses token_usage from report_usage")
			},
		},
		{
			name: "promote_to_autonomous",
			tool: "promote_to_autonomous",
			path: "root",
			setup: func(t *testing.T, env *testEnv) map[string]any {
				t.Helper()
				card := newLoggedCard(t, env, "Slim promote")

				return map[string]any{"card_id": card.ID, "agent_id": "human:tester"}
			},
		},
		{
			name: "increment_review_attempts",
			tool: "increment_review_attempts",
			path: "card",
			setup: func(t *testing.T, env *testEnv) map[string]any {
				t.Helper()
				card := claimLoggedCard(t, env, "Slim attempts")

				return map[string]any{"card_id": card.ID, "agent_id": "agent-1"}
			},
			extra: func(t *testing.T, root map[string]any) {
				t.Helper()

				card, ok := root["card"].(map[string]any)
				require.True(t, ok)
				assert.EqualValues(t, 1, card["review_attempts"], "agent parses card.review_attempts")
			},
		},
		{
			name: "list_cards",
			tool: "list_cards",
			path: "cards",
			setup: func(t *testing.T, env *testEnv) map[string]any {
				t.Helper()
				newLoggedCard(t, env, "Slim list")

				return map[string]any{"project": "test-project"}
			},
		},
		{
			name: "get_ready_tasks",
			tool: "get_ready_tasks",
			path: "cards",
			setup: func(t *testing.T, env *testEnv) map[string]any {
				t.Helper()
				newLoggedCard(t, env, "Slim ready")

				return map[string]any{"project": "test-project"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setupMCP(t)
			args := tt.setup(t, env)

			result := callTool(t, env, tt.tool, args)
			require.False(t, result.IsError, "%s should not error", tt.tool)

			var root map[string]any
			unmarshalResult(t, result, &root)

			switch tt.path {
			case "root":
				assertSlimCardMap(t, root)
			case "card":
				card, ok := root["card"].(map[string]any)
				require.True(t, ok, "expected card object in %s result", tt.tool)
				assertSlimCardMap(t, card)
			case "cards":
				cards, ok := root["cards"].([]any)
				require.True(t, ok, "expected cards array in %s result", tt.tool)
				require.NotEmpty(t, cards)

				for _, c := range cards {
					m, ok := c.(map[string]any)
					require.True(t, ok)
					assertSlimCardMap(t, m)
				}
			}

			if tt.extra != nil {
				tt.extra(t, root)
			}
		})
	}
}

// TestHeartbeatReturnsSlimAck pins the heartbeat response to a minimal ack:
// skills check state on resume (stalled detection), nothing needs the card.
func TestHeartbeatReturnsSlimAck(t *testing.T) {
	env := setupMCP(t)
	card := claimLoggedCard(t, env, "Slim heartbeat")

	result := callTool(t, env, "heartbeat", map[string]any{
		"card_id": card.ID, "agent_id": "agent-1",
	})
	require.False(t, result.IsError)

	var ack map[string]any
	unmarshalResult(t, result, &ack)

	assert.Equal(t, card.ID, ack["card_id"])
	// Service-level ClaimCard does not auto-transition, so the card is still
	// in todo; what matters is that state is present for stall detection.
	assert.Equal(t, "todo", ack["state"])
	assert.Contains(t, ack, "last_heartbeat")
	assert.NotContains(t, ack, "body")
	assert.NotContains(t, ack, "activity_log")
	assert.NotContains(t, ack, "title")
}
