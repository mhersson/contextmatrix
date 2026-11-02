package knowledge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Answer is a provider answer after normalization: items is always a list,
// and every other field, known or not, is passed through unchanged.
type Answer map[string]any

var (
	ErrUnknownProvider = errors.New("knowledge provider not configured")
	ErrProviderAbsent  = errors.New("knowledge provider unavailable")
	ErrTimeout         = errors.New("knowledge provider timed out")
	ErrProviderFailed  = errors.New("knowledge provider call failed")
)

// normalizeAnswer returns the answer, the raw JSON for the audit log (also on
// error), and an error wrapping ErrProviderFailed. Type mismatches are
// rejected because CM's own output schema is validated by the SDK.
func normalizeAnswer(res *mcp.CallToolResult, maxBytes int) (Answer, json.RawMessage, error) {
	var raw json.RawMessage

	switch {
	case res == nil:
	case res.StructuredContent != nil:
		b, err := json.Marshal(res.StructuredContent)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: encode structured content: %v", ErrProviderFailed, err)
		}

		raw = b
	default:
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				raw = json.RawMessage(tc.Text)

				break
			}
		}
	}

	if len(raw) == 0 {
		return nil, raw, fmt.Errorf("%w: no structured or text content", ErrProviderFailed)
	}

	if len(raw) > maxBytes {
		return nil, raw, fmt.Errorf("%w: answer is %d bytes, over the %d byte cap", ErrProviderFailed, len(raw), maxBytes)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var a Answer
	if err := dec.Decode(&a); err != nil || a == nil {
		return nil, raw, fmt.Errorf("%w: answer is not a JSON object", ErrProviderFailed)
	}

	if err := normalizeFields(a); err != nil {
		return nil, raw, fmt.Errorf("%w: %v", ErrProviderFailed, err)
	}

	return a, raw, nil
}

func normalizeFields(a Answer) error {
	switch v := a["items"].(type) {
	case nil:
		a["items"] = []any{}
	case []any:
		if !allObjects(v) {
			return errors.New("items has a non-object entry")
		}
	default:
		return errors.New("items is not a list")
	}

	if v, ok := a["note"]; ok {
		if v == nil {
			delete(a, "note")
		} else if _, isStr := v.(string); !isStr {
			return errors.New("note is not a string")
		}
	}

	if v, ok := a["related"]; ok {
		if v == nil {
			delete(a, "related")
		} else if l, isList := v.([]any); !isList || !allObjects(l) {
			return errors.New("related is not a list of objects")
		}
	}

	if v, ok := a["missing"]; ok {
		if v == nil {
			delete(a, "missing")
		} else if l, isList := v.([]any); !isList || !allStrings(l) {
			return errors.New("missing is not a list of strings")
		}
	}

	return nil
}

func allObjects(l []any) bool {
	for _, e := range l {
		if _, ok := e.(map[string]any); !ok {
			return false
		}
	}

	return true
}

func allStrings(l []any) bool {
	for _, e := range l {
		if _, ok := e.(string); !ok {
			return false
		}
	}

	return true
}
