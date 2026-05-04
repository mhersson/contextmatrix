package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
)

type mcpClient struct {
	baseURL     string
	apiKey      string
	agentID     string
	http        *http.Client
	nextID      atomic.Uint64
	sessionID   string
	initialized bool
}

func newMCP(baseURL, apiKey, agentID string) *mcpClient {
	if !strings.HasSuffix(baseURL, "/mcp") {
		baseURL = strings.TrimRight(baseURL, "/") + "/mcp"
	}
	return &mcpClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		agentID: agentID,
		http:    &http.Client{},
	}
}

// initialize performs the MCP initialize handshake and captures the session ID.
// It must be called once before any tool calls.
func (m *mcpClient) initialize() error {
	envelope := map[string]any{
		"jsonrpc": "2.0",
		"id":      0,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "stub-claude", "version": "0.0"},
		},
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal initialize: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, m.baseURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build initialize request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if m.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.apiKey)
	}

	resp, err := m.http.Do(req)
	if err != nil {
		return fmt.Errorf("post initialize: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read initialize body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("mcp initialize http %d: %s", resp.StatusCode, raw)
	}

	m.sessionID = resp.Header.Get("Mcp-Session-Id")
	m.initialized = true

	// Consume the response (ignore result — we only needed the session ID).
	payload := extractMCPPayload(resp.Header.Get("Content-Type"), raw)
	var rpc struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &rpc); err != nil {
		return fmt.Errorf("decode initialize response: %w (body: %s)", err, raw)
	}
	if rpc.Error != nil {
		return fmt.Errorf("mcp initialize: %s (code %d)", rpc.Error.Message, rpc.Error.Code)
	}

	// Send notifications/initialized — fire and forget (no id field).
	notification := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	notifReq, err := http.NewRequest(http.MethodPost, m.baseURL, bytes.NewReader(notification))
	if err != nil {
		return fmt.Errorf("build initialized notification: %w", err)
	}
	notifReq.Header.Set("Content-Type", "application/json")
	notifReq.Header.Set("Accept", "application/json, text/event-stream")
	if m.apiKey != "" {
		notifReq.Header.Set("Authorization", "Bearer "+m.apiKey)
	}
	if m.sessionID != "" {
		notifReq.Header.Set("Mcp-Session-Id", m.sessionID)
	}

	notifResp, err := m.http.Do(notifReq)
	if err != nil {
		// Non-fatal: server may not respond to notifications.
		return nil
	}
	notifResp.Body.Close()

	return nil
}

// extractMCPPayload unwraps SSE-framed responses. If contentType starts with
// "text/event-stream", it extracts the first "data: ..." line's payload.
// Otherwise the body is returned as-is.
func extractMCPPayload(contentType string, body []byte) []byte {
	if !strings.HasPrefix(contentType, "text/event-stream") {
		return body
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "data: ") {
			return []byte(strings.TrimPrefix(line, "data: "))
		}
		if strings.HasPrefix(line, "data:") {
			return []byte(strings.TrimPrefix(line, "data:"))
		}
	}
	return body
}

// callTool invokes the named tool with the given arguments and returns
// the decoded "result" content. Errors are returned as Go errors;
// JSON-RPC errors are wrapped as fmt.Errorf("mcp: ...").
func (m *mcpClient) callTool(name string, args map[string]any) (json.RawMessage, error) {
	if !m.initialized {
		if err := m.initialize(); err != nil {
			return nil, fmt.Errorf("initialize: %w", err)
		}
	}

	id := m.nextID.Add(1)
	envelope := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, m.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if m.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.apiKey)
	}
	if m.agentID != "" {
		req.Header.Set("X-Agent-ID", m.agentID)
	}
	if m.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", m.sessionID)
	}

	resp, err := m.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("mcp http %d: %s", resp.StatusCode, raw)
	}

	payload := extractMCPPayload(resp.Header.Get("Content-Type"), raw)

	var rpc struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &rpc); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, raw)
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("mcp: %s (code %d)", rpc.Error.Message, rpc.Error.Code)
	}

	// go-sdk embeds tool-level errors in the result as isError=true rather
	// than returning a JSON-RPC error. Check for this and surface it.
	if len(rpc.Result) > 0 {
		var toolResult struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(rpc.Result, &toolResult); err == nil && toolResult.IsError {
			msg := "tool error"
			for _, c := range toolResult.Content {
				if c.Type == "text" && c.Text != "" {
					msg = c.Text
					break
				}
			}
			return nil, fmt.Errorf("mcp tool error: %s", msg)
		}
	}

	return rpc.Result, nil
}

// Tiny convenience wrappers — six tools the stub uses.

func (m *mcpClient) GetCard(project, cardID string) (string, error) {
	res, err := m.callTool("get_card", map[string]any{
		"project": project, "card_id": cardID,
	})
	if err != nil {
		return "", err
	}
	// MCP tool results are wrapped as {"content":[{"type":"text","text":"<JSON>"}]}.
	// Peel that, then JSON-decode the inner card object to read its body.
	var envelope struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(res, &envelope); err != nil {
		return "", fmt.Errorf("decode tool envelope: %w", err)
	}
	for _, block := range envelope.Content {
		if block.Type != "text" || block.Text == "" {
			continue
		}
		var card struct {
			Body string `json:"body"`
		}
		if err := json.Unmarshal([]byte(block.Text), &card); err != nil {
			return "", fmt.Errorf("decode card body: %w (text: %.200s)", err, block.Text)
		}
		return card.Body, nil
	}
	return "", nil
}

func (m *mcpClient) ClaimCard(project, cardID string) error {
	_, err := m.callTool("claim_card", map[string]any{
		"project": project, "card_id": cardID, "agent_id": m.agentID,
	})
	return err
}

func (m *mcpClient) Heartbeat(project, cardID string) error {
	_, err := m.callTool("heartbeat", map[string]any{
		"project": project, "card_id": cardID, "agent_id": m.agentID,
	})
	return err
}

func (m *mcpClient) TransitionCard(project, cardID, toState string) error {
	_, err := m.callTool("transition_card", map[string]any{
		"project": project, "card_id": cardID, "new_state": toState, "agent_id": m.agentID,
	})
	return err
}

func (m *mcpClient) ReleaseCard(project, cardID string) error {
	_, err := m.callTool("release_card", map[string]any{
		"project": project, "card_id": cardID, "agent_id": m.agentID,
	})
	return err
}

func (m *mcpClient) AddLog(project, cardID, msg string) error {
	_, err := m.callTool("add_log", map[string]any{
		"project": project, "card_id": cardID, "message": msg, "action": "note", "agent_id": m.agentID,
	})
	return err
}
