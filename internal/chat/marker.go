package chat

import (
	"encoding/json"
	"fmt"

	protocol "github.com/mhersson/contextmatrix-protocol"
)

// EventKindMarker is the persisted Message.Kind / SSE DataKind of an
// architect stage divider or sheet reference.
const EventKindMarker = "marker"

// markerEnvelope is the persisted content of a marker row.
type markerEnvelope struct {
	Text   string              `json:"text"`
	Marker *protocol.LogMarker `json:"marker"`
}

// markerContent marshals a marker row within maxMessageBytes: the fallback
// text is cut first and Marker.Note only if the marker alone still
// overflows. The caller's marker is never modified.
func markerContent(text string, mk *protocol.LogMarker) (string, error) {
	return fitEnvelope(text, mk.Note, func(text, note string) (string, error) {
		cp := *mk
		cp.Note = note

		b, err := json.Marshal(markerEnvelope{Text: text, Marker: &cp})
		if err != nil {
			return "", fmt.Errorf("marshal marker envelope: %w", err)
		}

		return string(b), nil
	})
}
