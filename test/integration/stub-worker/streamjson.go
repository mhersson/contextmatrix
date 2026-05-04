package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// emitSystemInit writes the synthetic init frame the runner expects as
// the first event of any stream.
func emitSystemInit(w io.Writer, model, sessionID string) error {
	return writeJSONLine(w, map[string]any{
		"type":       "system",
		"subtype":    "init",
		"session_id": sessionID,
		"model":      model,
	})
}

// emitText writes a single text content frame wrapped in the assistant
// envelope the runner's logparser expects: {type:"assistant",
// message:{content:[{type:"text",text:"..."}]}}.
func emitText(w io.Writer, text string) error {
	return writeJSONLine(w, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": text},
			},
		},
	})
}

// emitToolUse writes a tool_use block inside the assistant envelope.
// Real Claude emits MCP tool calls with name "mcp__<server>__<tool>";
// the runner's logparser drops mcp__-prefixed tool_uses (they're tracked
// via the MCP server-side log). Tool name passed here should match
// whatever the test wants to surface to the operator.
func emitToolUse(w io.Writer, id, name string, input any) error {
	return writeJSONLine(w, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []map[string]any{
				{"type": "tool_use", "id": id, "name": name, "input": input},
			},
		},
	})
}

// emitToolResult writes a tool_result frame as a user-message envelope,
// matching real claude's reply-to-tool shape. The runner's logparser
// only pays attention to assistant frames, so this is informational for
// transcripts/operators only.
func emitToolResult(w io.Writer, toolUseID string, content any) error {
	return writeJSONLine(w, map[string]any{
		"type": "user",
		"message": map[string]any{
			"content": []map[string]any{
				{"type": "tool_result", "tool_use_id": toolUseID, "content": content},
			},
		},
	})
}

// emitResult writes the terminal result frame.
func emitResult(w io.Writer, summary string) error {
	return writeJSONLine(w, map[string]any{
		"type":    "result",
		"subtype": "success",
		"result":  summary,
	})
}

func writeJSONLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal stream-json: %w", err)
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}
