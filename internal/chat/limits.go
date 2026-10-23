package chat

import (
	"errors"
	"fmt"
)

const (
	// MaxChatMessageBytes is the per-message limit for card and chat sessions.
	MaxChatMessageBytes = 8192
	// MaxBlueprintMessageBytes is the per-message limit for blueprint sessions.
	MaxBlueprintMessageBytes = 64 * 1024
)

// ErrMessageTooLarge is wrapped by every refusal of an oversized user message.
var ErrMessageTooLarge = errors.New("chat: message too large")

// MaxMessageBytes returns the user-message limit in bytes for a session kind.
func MaxMessageBytes(kind SessionKind) int {
	if kind == KindArchitect {
		return MaxBlueprintMessageBytes
	}

	return MaxChatMessageBytes
}

func checkMessageSize(kind SessionKind, n int) error {
	if limit := MaxMessageBytes(kind); n > limit {
		return fmt.Errorf("%w: %d bytes, the limit is %d", ErrMessageTooLarge, n, limit)
	}

	return nil
}
