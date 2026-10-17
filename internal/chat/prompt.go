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

// promptContent marshals a prompt row within maxMessageBytes. The envelope
// text is only the fallback for a row CM renders from Prompt.Text, so it is
// cut first and Prompt.Text only if it alone still overflows. Text is cut,
// never the JSON, so a stored envelope always parses; the caller's prompt is
// never modified.
func promptContent(text string, p *protocol.LogPrompt) (string, error) {
	env, err := marshalPrompt(text, p)
	if err != nil || len(env) <= maxMessageBytes {
		return env, err
	}

	content, err := fitCut(len(text), func(n int) (string, error) {
		return marshalPrompt(cutText(text, n), p)
	})
	if p == nil || p.Text == "" || err == nil {
		return content, err
	}

	if !errors.Is(err, errPromptTooLarge) {
		return "", err
	}

	// Content is already at its minimum: keep only the marker, or nothing if it was empty.
	minimal := cutText(text, 0)

	return fitCut(len(p.Text), func(n int) (string, error) {
		cp := *p
		cp.Text = cutText(p.Text, n)

		return marshalPrompt(minimal, &cp)
	})
}

// fitCut binary-searches the largest cut length below size whose envelope
// fits. A binary search, because escaping makes the encoded size nonlinear in
// the cut.
func fitCut(size int, marshal func(n int) (string, error)) (string, error) {
	lo, hi := 0, size-1
	for lo < hi {
		mid := (lo + hi + 1) / 2

		probe, err := marshal(mid)
		if err != nil {
			return "", err
		}

		if len(probe) <= maxMessageBytes {
			lo = mid
		} else {
			hi = mid - 1
		}
	}

	env, err := marshal(lo)
	if err != nil {
		return "", err
	}

	if len(env) > maxMessageBytes {
		return "", errPromptTooLarge
	}

	return env, nil
}

func cutText(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return runePrefix(s, n) + truncationMarker
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
