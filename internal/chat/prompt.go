package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	protocol "github.com/mhersson/contextmatrix-protocol"
)

// EventKindPrompt is the persisted Message.Kind / SSE DataKind of an
// architect question or gate. The browser derives whether it is still open.
const EventKindPrompt = "prompt"

// promptEnvelope is the persisted content of a prompt row.
type promptEnvelope struct {
	Text   string              `json:"text"`
	Prompt *protocol.LogPrompt `json:"prompt"`
}

var errPromptTooLarge = errors.New("prompt actions exceed the message cap")

// promptContent marshals a prompt row within maxMessageBytes. The text is cut,
// never the JSON, so a stored envelope always parses.
func promptContent(text string, p *protocol.LogPrompt) (string, error) {
	env, err := marshalPrompt(text, p)
	if err != nil || len(env) <= maxMessageBytes {
		return env, err
	}

	// The longest prefix whose envelope, marker included, fits. A binary
	// search, because escaping makes the encoded size nonlinear in the cut.
	lo, hi := 0, len(text)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2

		probe, err := marshalPrompt(runePrefix(text, mid)+truncationMarker, p)
		if err != nil {
			return "", err
		}

		if len(probe) <= maxMessageBytes {
			lo = mid
		} else {
			hi = mid - 1
		}
	}

	env, err = marshalPrompt(runePrefix(text, lo)+truncationMarker, p)
	if err != nil {
		return "", err
	}

	if len(env) > maxMessageBytes {
		return "", errPromptTooLarge
	}

	return env, nil
}

func marshalPrompt(text string, p *protocol.LogPrompt) (string, error) {
	b, err := json.Marshal(promptEnvelope{Text: text, Prompt: p})
	if err != nil {
		return "", fmt.Errorf("marshal prompt envelope: %w", err)
	}

	return string(b), nil
}

// runePrefix returns text cut to at most n bytes at a rune boundary; n < len(text).
func runePrefix(text string, n int) string {
	for n > 0 && !utf8.RuneStart(text[n]) {
		n--
	}

	return text[:n]
}
