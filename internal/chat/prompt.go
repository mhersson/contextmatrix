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

var errEnvelopeTooLarge = errors.New("envelope exceeds the message cap")

// promptContent marshals a prompt row within maxMessageBytes. The envelope
// text is only the fallback for a row CM renders from Prompt.Text, so it is
// cut first and Prompt.Text only if it alone still overflows. Text is cut,
// never the JSON, so a stored envelope always parses; the caller's prompt is
// never modified.
func promptContent(text string, p *protocol.LogPrompt) (string, error) {
	if p == nil {
		return fitEnvelope(text, "", func(text, _ string) (string, error) { return marshalPrompt(text, nil) })
	}

	return fitEnvelope(text, p.Text, func(text, inner string) (string, error) {
		cp := *p
		cp.Text = inner

		return marshalPrompt(text, &cp)
	})
}

// fitEnvelope marshals an envelope within maxMessageBytes. It cuts the
// fallback text first and the inner display text only if the envelope still
// overflows with the text at its minimum.
func fitEnvelope(text, inner string, marshal func(text, inner string) (string, error)) (string, error) {
	env, err := marshal(text, inner)
	if err != nil || len(env) <= maxMessageBytes {
		return env, err
	}

	content, err := fitCut(len(text), func(n int) (string, error) {
		return marshal(cutText(text, n), inner)
	})
	if inner == "" || !errors.Is(err, errEnvelopeTooLarge) {
		return content, err
	}

	// The text is already at its minimum: keep only the truncation suffix, or nothing if it was empty.
	minimal := cutText(text, 0)

	return fitCut(len(inner), func(n int) (string, error) {
		return marshal(minimal, cutText(inner, n))
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
		return "", errEnvelopeTooLarge
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
