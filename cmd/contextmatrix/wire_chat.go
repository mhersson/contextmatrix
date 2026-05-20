package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/chat"
	chatsqlite "github.com/mhersson/contextmatrix/internal/chat/sqlite"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/service"
)

// wireChat builds the chat subsystem: SQLite store, runner client, SSE hub,
// manager, idle reaper, warm-idle grace timers, and the startup reattach loop.
// Returns the manager, the hub, and a cleanup function the caller must defer.
func wireChat(
	ctx context.Context,
	cfg *config.Config,
	svc *service.CardService,
) (*chat.Manager, *chat.SSEHub, func(), error) {
	chatStore, err := chatsqlite.Open(cfg.Chat.DBPath)
	if err != nil {
		slog.Error("failed to open chat store", "path", cfg.Chat.DBPath, "error", err)

		return nil, nil, nil, err
	}

	slog.Info("chat store opened", "path", cfg.Chat.DBPath)

	var chatRunner chat.RunnerClient
	if cfg.Runner.Enabled {
		chatRunner = chat.NewRunnerClient(chat.RunnerClientConfig{
			BaseURL:   cfg.Runner.URL,
			HMACKey:   cfg.Runner.APIKey,
			MCPAPIKey: cfg.MCPAPIKey,
		})
	} else {
		// Nil runner causes nil-pointer panics at call sites. Use a no-op stub
		// that returns an error on every operation — chat features require runner.
		chatRunner = chatRunnerDisabled{}
	}

	chatHub := chat.NewSSEHub(128)

	primerPath := ""
	if cfg.WorkflowSkillsDir != "" {
		primerPath = filepath.Join(cfg.WorkflowSkillsDir, "chat-mode.md")
	}

	chatMgr := chat.NewManager(chat.Config{
		Store:              chatStore,
		Runner:             chatRunner,
		Clock:              clock.Real(),
		IdleTTL:            cfg.Chat.IdleTTL,
		MaxConcurrent:      cfg.Chat.MaxConcurrent,
		Hub:                chatHub,
		ResumeBudgetTokens: cfg.Chat.ResumeBudgetTokens,
		RehydrationTimeout: cfg.Chat.RehydrationTimeout,
		DefaultModel:       cfg.Chat.DefaultModel,
		PrimerPath:         primerPath,
		ResolveRepoURL: func(rctx context.Context, project string) (string, error) {
			p, err := svc.GetProject(rctx, project)
			if err != nil {
				return "", err
			}

			if p.Repo != "" {
				return p.Repo, nil
			}

			repos := p.EffectiveRepos()
			if len(repos) > 0 {
				return repos[0].URL, nil
			}

			return "", nil
		},
	})
	go chat.NewIdleReaper(chatMgr, time.Minute).Run(ctx)

	// 30s grace timer: last subscriber drop → flip session to warm-idle.
	// A new subscriber within 30s cancels the flip.
	var graceTimers sync.Map // sessionID → *time.Timer

	chatHub.OnLastUnsubscribe = func(sessionID string) {
		if existing, ok := graceTimers.LoadAndDelete(sessionID); ok {
			existing.(*time.Timer).Stop()
		}

		timer := time.AfterFunc(30*time.Second, func() {
			// If the entry is still in the map it means no new subscriber
			// arrived during the grace window — proceed with warm-idle.
			if _, loaded := graceTimers.LoadAndDelete(sessionID); !loaded {
				return
			}

			if err := chatMgr.MarkWarmIdle(ctx, sessionID); err != nil {
				slog.Warn("chat: warm-idle transition failed", "session_id", sessionID, "error", err)
			}
		})
		graceTimers.Store(sessionID, timer)
	}
	chatHub.OnSubscribe = func(sessionID string) {
		if t, ok := graceTimers.LoadAndDelete(sessionID); ok {
			t.(*time.Timer).Stop()
		}
		// A browser subscriber is a strong "I want this chat" signal.
		// Reattach the runner-log consumer if one isn't already bridging
		// /logs for this session — covers the case where CM restarted
		// while runner containers stayed alive, stranding their consumer
		// goroutines. No-op on cold/ending sessions. The returned Session
		// tells us the current status so we can skip a redundant GetSession
		// inside MarkActive when the session is already active.
		sess, err := chatMgr.Reattach(ctx, sessionID)
		if err != nil {
			slog.Warn("chat: reattach on subscribe failed",
				"session_id", sessionID, "error", err)
		}
		// Promote warm-idle back to active so the sidebar dot turns green.
		// Only needed when the session is warm-idle; skip when already active
		// to avoid a redundant GetSession round-trip inside MarkActive.
		// Best-effort: failure must never break the SSE handshake.
		if err == nil && sess.Status == chat.StatusWarmIdle {
			if err := chatMgr.MarkActive(ctx, sessionID); err != nil {
				slog.Warn("chat: mark-active on subscribe failed",
					"session_id", sessionID, "error", err)
			}
		}
	}

	slog.Info("chat manager initialized", "idle_ttl", cfg.Chat.IdleTTL, "max_concurrent", cfg.Chat.MaxConcurrent)

	// Resume runner-log consumers for sessions that survived a CM restart.
	// Without this, active/warm-idle sessions stay marked alive in the DB
	// while their consumer goroutines are gone (in-memory state lost), so
	// the UI can't see runner output even though the container is still
	// up. Reattach is idempotent and tolerant of dead containers — the
	// consumer exits on first /logs error and the reconcile sweep below
	// will flip orphaned sessions to cold.
	go func() {
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		for _, status := range []chat.Status{chat.StatusActive, chat.StatusWarmIdle} {
			sessions, err := chatMgr.ListSessions(rctx, chat.SessionFilter{Status: status})
			if err != nil {
				slog.Warn("chat: startup reattach list failed",
					"status", status, "error", err)

				continue
			}

			for _, s := range sessions {
				if _, err := chatMgr.Reattach(rctx, s.ID); err != nil {
					slog.Warn("chat: startup reattach failed",
						"session_id", s.ID, "error", err)
				}
			}
		}
	}()

	cleanup := func() {
		chatCloseCtx, chatCloseCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := chatMgr.Close(chatCloseCtx); err != nil {
			slog.Warn("chat manager close failed", "error", err)
		}

		chatCloseCancel()

		if err := chatStore.Close(); err != nil {
			slog.Warn("chat store close failed", "error", err)
		}
	}

	return chatMgr, chatHub, cleanup, nil
}
