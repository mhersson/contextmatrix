//go:build integration

package integration_test

import (
	"bytes"
	"strings"
	"testing"
)

// TestSummarizeWorkerStream covers the worker.raw.jsonl walker. Frames
// follow Claude's stream-json envelope shape: assistant/user wrappers
// with nested content blocks for text / tool_use / tool_result.
func TestSummarizeWorkerStream(t *testing.T) {
	raw := []byte(`{"type":"system","subtype":"init","session_id":"s1","model":"claude"}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"claim_card","input":{}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"claimed"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"heartbeat","input":{}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t3","name":"heartbeat","input":{}}]}}
{"type":"result","subtype":"success","result":"done"}
not-a-json-line
`)

	types, tools := summarizeWorkerStream(raw)

	wantTypes := map[string]int{
		"system":    1,
		"assistant": 4,
		"user":      1,
		"result":    1,
	}
	for k, v := range wantTypes {
		if types[k] != v {
			t.Errorf("types[%q] = %d, want %d", k, types[k], v)
		}
	}

	if tools["claim_card"] != 1 {
		t.Errorf("tools[claim_card] = %d, want 1", tools["claim_card"])
	}

	if tools["heartbeat"] != 2 {
		t.Errorf("tools[heartbeat] = %d, want 2", tools["heartbeat"])
	}
}

func TestSummarizeWorkerStreamEmpty(t *testing.T) {
	types, tools := summarizeWorkerStream(nil)
	if len(types) != 0 || len(tools) != 0 {
		t.Errorf("empty input should produce empty maps; got types=%v tools=%v", types, tools)
	}
}

func TestRenderCardsSection(t *testing.T) {
	cardsJSON := []byte(`[
		{"id":"INT-001","title":"Parent","state":"done","assigned_agent":"",
		 "token_usage":{"prompt_tokens":1500,"completion_tokens":800,"estimated_cost_usd":0.012},
		 "activity_log":[{"agent":"orchestrator","ts":"2026-05-04T10:00:00Z","action":"pushed","message":"Pushed to feat/x"}]},
		{"id":"INT-002","title":"Subtask A","parent":"INT-001","state":"done","assigned_agent":"",
		 "token_usage":{"prompt_tokens":600,"completion_tokens":400,"estimated_cost_usd":0.005}}
	]`)

	var b bytes.Buffer

	renderCardsSection(&b, cardsJSON)

	out := b.String()
	for _, want := range []string{"INT-001", "INT-002", "Parent", "Subtask A", "$0.0120", "parent: `INT-001`"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

// TestRenderCardsSectionEnvelope exercises the production path: the
// snapshot cleanup writes CM's {"items":[...]} response verbatim, and
// the renderer must extract Items.
func TestRenderCardsSectionEnvelope(t *testing.T) {
	cardsJSON := []byte(`{"items":[
		{"id":"INT-001","title":"Parent","state":"done","assigned_agent":"",
		 "token_usage":{"prompt_tokens":42,"completion_tokens":7,"estimated_cost_usd":0.001}}
	],"total":1}`)

	var b bytes.Buffer

	renderCardsSection(&b, cardsJSON)

	out := b.String()
	if !strings.Contains(out, "INT-001") {
		t.Errorf("envelope path lost cards: %s", out)
	}
}

func TestRenderCardsSectionEmpty(t *testing.T) {
	var b bytes.Buffer

	renderCardsSection(&b, nil)

	if !strings.Contains(b.String(), "cards.json not captured") {
		t.Errorf("expected sentinel for empty input, got: %s", b.String())
	}
}

func TestRenderActivityLogSection(t *testing.T) {
	cardsJSON := []byte(`[
		{"id":"INT-001","title":"P","state":"done","assigned_agent":"",
		 "activity_log":[
			{"agent":"orch","ts":"2026-05-04T10:00:00Z","action":"skill_engaged","skill":"harness-canary-skill"},
			{"agent":"orch","ts":"2026-05-04T10:05:00Z","action":"pushed","message":"feat/x"}
		 ]}
	]`)

	var b bytes.Buffer

	renderActivityLogSection(&b, cardsJSON)

	out := b.String()
	for _, want := range []string{"skill_engaged", "skill=harness-canary-skill", "pushed", "feat/x"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}
