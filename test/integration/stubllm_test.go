//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// The content-keyed matcher table and the SSE wire builders below are ported
// verbatim (with light renaming) from contextmatrix-agent's
// internal/worker/e2e_orchestrator_test.go `scriptedBackend`. They cannot be
// imported - they are _test.go-internal to that package - so the port is
// intentional duplication.
//
// MATCHER-SYNC WARNING: the matchers key on the agent orchestrator's phase
// persona preambles ("You are the planning agent", …). If the agent reworks a
// phase prompt, update the matcher here in lockstep or the scenario hangs on
// the "UNEXPECTED PROMPT" fallback. Keep every matcher in this one file.

// stubLLM is a scripted OpenAI-compatible endpoint. It binds 0.0.0.0 so worker
// containers reach it via host.docker.internal:<port>; the port is
// kernel-assigned. It serves POST /chat/completions from a reply function and
// 404s everything else (including /models - the worker's catalog fetch then
// degrades to priors and the pinned default model, mirroring the agent's own
// e2e).
type stubLLM struct {
	srv  *http.Server
	port int
}

// startStubLLM stands up the endpoint. reply maps a parsed request to a full
// SSE body. Every request's first user-message prefix is logged to the runlog.
func startStubLLM(t *testing.T, rl *runLog, name string, reply func(chatRequest) string) *stubLLM {
	t.Helper()

	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("stub LLM listen: %v", err)
	}

	port := ln.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		var req chatRequest

		_ = json.Unmarshal(body, &req)

		if rl != nil {
			first := firstUserContent(req)
			if len(first) > 120 {
				first = first[:120]
			}

			line := fmt.Sprintf("%s: /chat/completions first_user=%q", name, first)
			rl.writeLine("stubllm", line)
			_, _ = rl.stubllmSink.WriteString(line + "\n")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, reply(req))
	})

	srv := &http.Server{Handler: mux}

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			if rl != nil {
				rl.writeLine("stubllm", "serve: "+err.Error())
			}
		}
	}()

	t.Cleanup(func() { _ = srv.Close() })

	if rl != nil {
		rl.writeLine("stubllm", fmt.Sprintf("%s: serving /chat/completions on 0.0.0.0:%d", name, port))
	}

	return &stubLLM{srv: srv, port: port}
}

// chatRequest is the subset of the OpenAI request body the matchers inspect.
type chatRequest struct {
	Messages []struct {
		Role       string `json:"role"`
		Content    string `json:"content"`
		ToolCallID string `json:"tool_call_id"`
	} `json:"messages"`
}

func firstUserContent(req chatRequest) string {
	for _, m := range req.Messages {
		if m.Role == "user" {
			return m.Content
		}
	}

	return ""
}

// scriptedBackend is the agent happy-path matcher. Ported from the agent repo.
// approveImmediately=true approves review on the first synthesis round.
type scriptedBackend struct {
	mu sync.Mutex

	approveImmediately bool
	fixFile            string

	planCost       float64
	coderCost      float64
	documentCost   float64
	specialistCost float64
	synthesisCost  float64
	fixCost        float64

	synthesisCalls int
	totalCost      float64
	requests       int
}

// reply selects the SSE body for one request from the prompt and conversation
// state. Ported verbatim from the agent's scriptedBackend.reply.
func (b *scriptedBackend) reply(req chatRequest) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.requests++

	firstUser := ""
	hasToolResult := false

	for _, m := range req.Messages {
		if m.Role == "user" && firstUser == "" {
			firstUser = m.Content
		}

		if m.Role == "tool" || m.ToolCallID != "" {
			hasToolResult = true
		}
	}

	switch {
	case strings.Contains(firstUser, "You are the planning agent"):
		b.totalCost += b.planCost

		return ssePlan(b.planCost)

	case strings.Contains(firstUser, "You are the coding agent for one subtask"):
		if hasToolResult {
			b.totalCost += b.coderCost

			return sseCoderCommit(coderCommitFor(firstUser), b.coderCost)
		}

		return sseWriteTool("call_code", writeArgsFor(firstUser), 0)

	case strings.Contains(firstUser, "You are a code-review specialist"):
		b.totalCost += b.specialistCost

		return sseStop(specialistFindings, b.specialistCost)

	case strings.Contains(firstUser, "You are the review synthesizer"):
		b.synthesisCalls++
		b.totalCost += b.synthesisCost

		if b.approveImmediately || b.synthesisCalls >= 2 {
			return sseStop(verdictApproved, b.synthesisCost)
		}

		return sseStop(verdictReject(b.fixFile), b.synthesisCost)

	case strings.Contains(firstUser, "You are the coding agent addressing review feedback"):
		if hasToolResult {
			b.totalCost += b.fixCost

			return sseStop("Applied the fix.", b.fixCost)
		}

		return sseWriteTool("call_fix", writeArg(b.fixFile, "package main\n\n// fixed per review\n"), 0)

	case strings.Contains(firstUser, "You are the documentation agent"):
		b.totalCost += b.documentCost

		return sseStop("No external documentation is needed for this change.", b.documentCost)

	case strings.Contains(firstUser, "You are writing the pull request description"):
		return sseStop("## What\nWork.\n", 0)

	default:
		return sseStop("UNEXPECTED PROMPT", 0)
	}
}

// snapshot returns a stable copy of the recorded counters under the lock.
func (b *scriptedBackend) snapshot() (synthesisCalls, requests int, totalCost float64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.synthesisCalls, b.requests, b.totalCost
}

// --- SSE wire builders (the exact format the harness LLM client parses) -----

func usageWithCost(cost float64) string {
	return `"usage":{"prompt_tokens":100,"completion_tokens":40,"total_tokens":140,"cost":` +
		jsonNumber(cost) + `}`
}

func sseStop(content string, cost float64) string {
	return `data: {"model":"default/model","choices":[{"delta":{"content":` + jsonString(content) +
		`},"finish_reason":"stop"}],` + usageWithCost(cost) + "}\n" +
		"data: [DONE]\n"
}

func sseWriteTool(callID, args string, cost float64) string {
	var sb strings.Builder

	sb.WriteString(`data: {"model":"default/model","choices":[{"delta":{"tool_calls":[` +
		`{"index":0,"id":"` + callID + `","type":"function","function":{"name":"write","arguments":""}}]}}]}` + "\n")
	sb.WriteString(`data: {"choices":[{"delta":{"tool_calls":[` +
		`{"index":0,"function":{"arguments":` + jsonString(args) + `}}]}}]}` + "\n")
	sb.WriteString(`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}],` + usageWithCost(cost) + "}\n")
	sb.WriteString("data: [DONE]\n")

	return sb.String()
}

func sseFinish(commitMsg string, cost float64) string {
	args, _ := json.Marshal(map[string]string{"commit_message": commitMsg})

	var sb strings.Builder

	sb.WriteString(`data: {"model":"default/model","choices":[{"delta":{"tool_calls":[` +
		`{"index":0,"id":"call_finish","type":"function","function":{"name":"finish","arguments":""}}]}}]}` + "\n")
	sb.WriteString(`data: {"choices":[{"delta":{"tool_calls":[` +
		`{"index":0,"function":{"arguments":` + jsonString(string(args)) + `}}]}}]}` + "\n")
	sb.WriteString(`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}],` + usageWithCost(cost) + "}\n")
	sb.WriteString("data: [DONE]\n")

	return sb.String()
}

// ssePlan is the planner's stop turn: a two-subtask plan with one dependency
// edge (subtask 1 depends on subtask 0).
func ssePlan(cost float64) string {
	plan := `{"card_tier":"moderate","subtasks":[` +
		`{"title":"Add feature A","description":"Files: feature_a.txt. Create feature_a.txt.","depends_on":[],"tier":"simple"},` +
		`{"title":"Add feature B","description":"Files: feature_b.txt. Create feature_b.txt after A.","depends_on":[0],"tier":"moderate"}` +
		`]}`

	return sseStop(plan, cost)
}

func sseCoderCommit(commitMsg string, cost float64) string {
	return sseFinish(commitMsg, cost)
}

const specialistFindings = "Strengths: clear change.\nNo concerns.\nVerdict: looks good."

const verdictApproved = `{"approved":true,"summary":"All three lenses clean.","fixes":[]}`

func verdictReject(file string) string {
	return `{"approved":false,"summary":"One fix required.","fixes":[` +
		`{"file":"` + file + `","issue":"needs a tweak","suggestion":"adjust it"}]}`
}

func writeArgsFor(prompt string) string {
	if strings.Contains(prompt, "Add feature B") {
		return writeArg("feature_b.txt", "feature B\n")
	}

	return writeArg("feature_a.txt", "feature A\n")
}

func coderCommitFor(prompt string) string {
	if strings.Contains(prompt, "Add feature B") {
		return "feat(app): add feature b"
	}

	return "feat(app): add feature a"
}

func writeArg(path, content string) string {
	b, _ := json.Marshal(map[string]string{"path": path, "content": content})

	return string(b)
}

func jsonNumber(f float64) string {
	b, _ := json.Marshal(f)

	return string(b)
}

// jsonString quotes s as a JSON string literal (ported from the agent's
// e2e_test.go - the content/args escaping the wire format requires).
func jsonString(s string) string {
	var b strings.Builder

	b.WriteByte('"')

	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}

	b.WriteByte('"')

	return b.String()
}
