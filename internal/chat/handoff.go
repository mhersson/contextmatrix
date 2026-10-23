package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	// EventKindHandoff is the persisted Message.Kind / SSE DataKind of the
	// system row a chat agent posts to hand its need to the architect.
	EventKindHandoff = "handoff"
	// EventKindHandoffNeed is the Message.Kind of the user row that carries a
	// handed-over need into a blueprint session.
	EventKindHandoffNeed = "handoff_need"
	// MaxHandoffTitleBytes bounds a hand-off title; it becomes the blueprint title.
	MaxHandoffTitleBytes = 256
)

// maxHandoffRowBytes bounds the stored envelope; JSON escapes one byte to at most six.
const maxHandoffRowBytes = 6*(MaxBlueprintMessageBytes+MaxHandoffTitleBytes) + 16*1024

// ErrHandoffReplaced reports a create from a hand-off a newer one superseded.
var ErrHandoffReplaced = errors.New("chat: a newer hand-off replaced this one")

// Handoff is the content of a handoff row.
type Handoff struct {
	Title    string   `json:"title"`
	Need     string   `json:"need"`
	Projects []string `json:"projects"`
}

// PostHandoff appends a handoff system row to a plain chat and publishes it.
func (m *Manager) PostHandoff(ctx context.Context, sessionID string, h Handoff) (Message, error) {
	sess, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return Message{}, fmt.Errorf("chat: PostHandoff: %w", err)
	}

	if sess.Kind != KindChat {
		return Message{}, fmt.Errorf("%w: only a plain chat can hand off to the architect", ErrInvalidSessionInput)
	}

	if m.architectBackend == nil {
		return Message{}, fmt.Errorf("%w: this ContextMatrix has no architect configured", ErrKindUnavailable)
	}

	if strings.TrimSpace(h.Title) == "" {
		return Message{}, fmt.Errorf("%w: the title is empty: give the blueprint a short title", ErrInvalidSessionInput)
	}

	if n := len(h.Title); n > MaxHandoffTitleBytes {
		return Message{}, fmt.Errorf("%w: the title is %d bytes, the limit is %d: shorten it and call again",
			ErrInvalidSessionInput, n, MaxHandoffTitleBytes)
	}

	if strings.TrimSpace(h.Need) == "" {
		return Message{}, fmt.Errorf("%w: the need is empty: write what the architect should plan", ErrInvalidSessionInput)
	}

	if n := len(h.Need); n > MaxBlueprintMessageBytes {
		return Message{}, fmt.Errorf("%w: the need is %d bytes, the limit is %d: tighten it and call again",
			ErrMessageTooLarge, n, MaxBlueprintMessageBytes)
	}

	if len(h.Projects) == 0 {
		return Message{}, fmt.Errorf("%w: name at least one project", ErrInvalidSessionInput)
	}

	projects := make([]string, 0, len(h.Projects))
	seen := make(map[string]bool, len(h.Projects))

	for _, p := range h.Projects {
		if seen[p] {
			continue
		}

		seen[p] = true

		if m.resolveProjectExec != nil {
			if _, err := m.resolveProjectExec(ctx, p); err != nil {
				m.logger.Warn("chat: PostHandoff: project lookup failed", "project", p, "error", err)

				return Message{}, fmt.Errorf("%w: unknown project %q: ask the operator, who may need to create it in ContextMatrix first",
					ErrInvalidSessionInput, p)
			}
		}

		projects = append(projects, p)
	}

	h.Projects = projects

	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(h); err != nil {
		return Message{}, fmt.Errorf("chat: PostHandoff: encode: %w", err)
	}

	content := strings.TrimSuffix(buf.String(), "\n")
	if len(content) > maxHandoffRowBytes {
		return Message{}, fmt.Errorf("%w: the hand-off is %d bytes encoded, the limit is %d: tighten it and call again",
			ErrMessageTooLarge, len(content), maxHandoffRowBytes)
	}

	msg, err := m.appendMessageWithKind(ctx, sessionID, RoleSystem, content, EventKindHandoff)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return Message{}, err
		}

		return Message{}, fmt.Errorf("chat: PostHandoff: %w", err)
	}

	if m.hub != nil {
		m.hub.Publish(sessionID, SSEEvent{
			Seq:              msg.Seq,
			Role:             RoleSystem,
			Content:          content,
			DataKind:         EventKindHandoff,
			RehydrationPhase: msg.RehydrationPhase,
		})
	}

	return msg, nil
}

// checkOrigin validates the hand-off a blueprint create names, if any.
func (m *Manager) checkOrigin(ctx context.Context, kind SessionKind, in CreateInput) error {
	if in.OriginChat == "" {
		if in.OriginSeq != 0 {
			return fmt.Errorf("%w: origin_seq needs origin_chat", ErrInvalidSessionInput)
		}

		return nil
	}

	if kind != KindArchitect {
		return fmt.Errorf("%w: only a blueprint session starts from a hand-off", ErrInvalidSessionInput)
	}

	if in.OriginSeq <= 0 {
		return fmt.Errorf("%w: origin_seq must name the hand-off row", ErrInvalidSessionInput)
	}

	origin, err := m.store.GetSession(ctx, in.OriginChat)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return fmt.Errorf("%w: unknown origin chat", ErrInvalidSessionInput)
		}

		return fmt.Errorf("chat: CreateSession: origin chat: %w", err)
	}

	if origin.Kind != KindChat {
		return fmt.Errorf("%w: the origin must be a plain chat", ErrInvalidSessionInput)
	}

	newest, err := m.store.LastSeqOfKind(ctx, origin.ID, EventKindHandoff)
	if err != nil {
		return fmt.Errorf("chat: CreateSession: newest hand-off: %w", err)
	}

	switch {
	case newest == 0:
		return fmt.Errorf("%w: the origin chat has no hand-off", ErrInvalidSessionInput)
	case newest != in.OriginSeq:
		return ErrHandoffReplaced
	}

	return nil
}
