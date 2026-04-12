package jira

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// WriteBackHandler listens for card state changes and posts comments back to Jira.
type WriteBackHandler struct {
	client *Client
	store  *storage.FilesystemStore
	bus    *events.Bus
	wg     sync.WaitGroup
}

// NewWriteBackHandler creates a new write-back handler.
func NewWriteBackHandler(client *Client, store *storage.FilesystemStore, bus *events.Bus) *WriteBackHandler {
	return &WriteBackHandler{
		client: client,
		store:  store,
		bus:    bus,
	}
}

// Start subscribes to the event bus and begins processing card state changes.
func (h *WriteBackHandler) Start(ctx context.Context) {
	ch, unsub := h.bus.Subscribe()

	h.wg.Add(1)

	go func() {
		defer h.wg.Done()
		defer unsub()

		for {
			select {
			case <-ctx.Done():
				return
			case event := <-ch:
				h.handleEvent(ctx, event)
			}
		}
	}()
}

// Wait blocks until the background goroutine has finished.
func (h *WriteBackHandler) Wait() {
	h.wg.Wait()
}

func (h *WriteBackHandler) handleEvent(ctx context.Context, event events.Event) {
	if event.Type != events.CardStateChanged {
		return
	}

	newState, _ := event.Data["new_state"].(string)
	if newState != board.StateDone {
		return
	}

	// Load the card to check its source.
	card, err := h.store.GetCard(ctx, event.Project, event.CardID)
	if err != nil {
		slog.Warn("jira writeback: get card", "card", event.CardID, "error", err)

		return
	}

	if card.Source == nil || card.Source.System != "jira" {
		return
	}

	// Post comment on the individual Jira issue.
	comment := formatCardComment(card)
	if err := h.client.PostComment(ctx, card.Source.ExternalID, comment); err != nil {
		slog.Warn("jira writeback: post comment",
			"card", event.CardID, "jira_key", card.Source.ExternalID, "error", err)
	} else {
		slog.Info("jira writeback: comment posted",
			"card", event.CardID, "jira_key", card.Source.ExternalID)
	}

	// Check if all Jira-sourced cards in this project are done.
	if allDone, epicKey := h.checkEpicCompletion(ctx, event.Project); allDone && epicKey != "" {
		summary := h.formatEpicComment(ctx, event.Project)
		if err := h.client.PostComment(ctx, epicKey, summary); err != nil {
			slog.Warn("jira writeback: post epic comment",
				"project", event.Project, "epic", epicKey, "error", err)
		} else {
			slog.Info("jira writeback: epic completion comment posted",
				"project", event.Project, "epic", epicKey)
		}
	}
}

// checkEpicCompletion checks if all Jira-sourced cards in the project are in a terminal state.
// Returns true and the epic key if all done, false otherwise.
func (h *WriteBackHandler) checkEpicCompletion(ctx context.Context, project string) (bool, string) {
	cfg, err := h.store.GetProject(ctx, project)
	if err != nil || cfg.Jira == nil {
		return false, ""
	}

	cards, err := h.store.ListCards(ctx, project, storage.CardFilter{})
	if err != nil {
		slog.Warn("jira writeback: list cards for epic check", "project", project, "error", err)

		return false, ""
	}

	jiraCardCount := 0

	for _, c := range cards {
		if c.Source == nil || c.Source.System != "jira" {
			continue
		}

		jiraCardCount++

		if c.State != board.StateDone && c.State != board.StateNotPlanned {
			return false, ""
		}
	}

	if jiraCardCount == 0 {
		return false, ""
	}

	return true, cfg.Jira.EpicKey
}

// formatCardComment creates the Jira comment for a single card completion.
func formatCardComment(card *board.Card) string {
	return fmt.Sprintf("ContextMatrix: Task completed (%s)\n\n%s", card.ID, card.Title)
}

// formatEpicComment creates the Jira comment for epic completion.
func (h *WriteBackHandler) formatEpicComment(ctx context.Context, project string) string {
	cards, err := h.store.ListCards(ctx, project, storage.CardFilter{})
	if err != nil {
		return "ContextMatrix: All tasks completed"
	}

	var (
		doneCount, notPlannedCount int
		lines                      []string
	)

	for _, c := range cards {
		if c.Source == nil || c.Source.System != "jira" {
			continue
		}

		switch c.State {
		case board.StateDone:
			doneCount++

			lines = append(lines, fmt.Sprintf("- %s (done)", c.Title))
		case board.StateNotPlanned:
			notPlannedCount++

			lines = append(lines, fmt.Sprintf("- %s (not planned)", c.Title))
		}
	}

	var buf strings.Builder
	buf.WriteString("ContextMatrix: All tasks completed\n\n")
	fmt.Fprintf(&buf, "%d tasks completed", doneCount)

	if notPlannedCount > 0 {
		fmt.Fprintf(&buf, ", %d not planned", notPlannedCount)
	}

	buf.WriteString(".\n\n")
	buf.WriteString(strings.Join(lines, "\n"))

	return buf.String()
}
