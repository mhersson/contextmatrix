package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mhersson/contextmatrix/internal/api"
	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/service"
)

// wireChat builds the chat subsystem: backend client, SSE hub, manager, idle
// reaper, warm-idle grace timers, and the startup reattach loop. The chat
// store is the shared operational store (ops.db), opened and owned by the
// caller — wireChat does not open or close it. Returns the manager, the hub,
// a cleanup function the caller must defer, and the resolved chat-backend
// api_key (empty when no chat backend is configured) — the caller wires this
// into RouterConfig.ChatWorkerAPIKey so GET /api/worker/git-credentials
// verifies bearer tokens against the exact key that minted them below.
func wireChat(
	ctx context.Context,
	cfg *config.Config,
	svc *service.CardService,
	chatStore chat.Store,
) (*chat.Manager, *chat.SSEHub, func(), string) {
	var chatBackend chat.Backend

	// chatDefaultModel is the cold-open fallback used when a session row has
	// an empty model: the chat backend entry's default_model (an OpenRouter
	// slug). Empty when no chat backend is configured — sends fail at open
	// time via the disabled stub anyway.
	var chatDefaultModel string

	// chatBackendEntry/chatBackendOK are hoisted out of the if-statement
	// (unlike a plain `if entry, ok := ...; ok {}`) because the resolved
	// entry's APIKey is also the worker-credentials bearer secret, needed
	// again below to build workerCredentialsToken and to return to the
	// caller for RouterConfig.ChatWorkerAPIKey.
	chatBackendEntry, chatBackendOK := cfg.ChatBackend()

	if chatBackendOK {
		chatBackend = chat.NewBackendClient(chat.BackendClientConfig{
			BaseURL:   chatBackendEntry.URL,
			HMACKey:   chatBackendEntry.APIKey,
			MCPAPIKey: cfg.MCPAPIKey,
		})

		chatDefaultModel = chatBackendEntry.DefaultModel
	} else {
		// Nil backend causes nil-pointer panics at call sites. Use a no-op
		// stub that errors on every operation — chat features require a
		// configured chat backend.
		chatBackend = chatBackendDisabled{}
	}

	// chatWorkerAPIKey is the secret behind WorkerCredentialsToken: empty
	// unless a chat backend is configured AND carries an api_key, matching
	// the router's registration gate for GET /api/worker/git-credentials
	// (see RouterConfig.ChatWorkerAPIKey).
	chatWorkerAPIKey := ""

	var workerCredentialsToken func(sessionID string) string

	if chatBackendOK && chatBackendEntry.APIKey != "" {
		chatWorkerAPIKey = chatBackendEntry.APIKey
		workerCredentialsToken = func(sessionID string) string {
			return api.WorkerCredentialsToken(chatWorkerAPIKey, sessionID)
		}
	}

	chatHub := chat.NewSSEHub(128)

	chatMgr := chat.NewManager(chat.Config{
		Store:                  chatStore,
		Backend:                chatBackend,
		Clock:                  clock.Real(),
		IdleTTL:                cfg.Chat.IdleTTL,
		MaxConcurrent:          cfg.Chat.MaxConcurrent,
		Hub:                    chatHub,
		ResumeBudgetTokens:     cfg.Chat.ResumeBudgetTokens,
		RehydrationTimeout:     cfg.Chat.RehydrationTimeout,
		DefaultModel:           chatDefaultModel,
		Pricer:                 svc,
		LLMEndpoint:            llmEndpointFromConfig(cfg.LLMEndpoint),
		WorkerCredentialsToken: workerCredentialsToken,
		ResolveProjectExec: func(rctx context.Context, project string) (chat.ProjectExecInfo, error) {
			p, err := svc.GetProject(rctx, project)
			if err != nil {
				return chat.ProjectExecInfo{}, err
			}

			info := chat.ProjectExecInfo{RepoURL: p.Repo}
			if info.RepoURL == "" {
				if repos := p.EffectiveRepos(); len(repos) > 0 {
					info.RepoURL = repos[0].URL
				}
			}

			// Chat uses runner_image regardless of remote_execution.enabled:
			// enabled gates autonomous card execution, while the image answers
			// "what toolchain does this project need", which applies to
			// interactive sessions identically.
			if p.RemoteExecution != nil {
				info.WorkerImage = p.RemoteExecution.RunnerImage
			}

			return info, nil
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
		// Reattach the worker-log consumer if one isn't already bridging
		// /logs for this session — covers the case where CM restarted
		// while worker containers stayed alive, stranding their consumer
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

	// Wire the chat cost summarizer so GetDashboard can populate the
	// "Chat cost · 30d" tile. Done after both chatMgr and svc are constructed.
	svc.SetChatCostSummarizer(chatMgr)

	// Resume worker-log consumers for sessions that survived a CM restart.
	// Without this, active/warm-idle sessions stay marked alive in the DB
	// while their consumer goroutines are gone (in-memory state lost), so
	// the UI can't see worker output even though the container is still
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

	// The chat store is the shared operational store, owned and closed by the
	// caller (defer opStore.Close() in main). Only the manager is torn down here.
	cleanup := func() {
		chatCloseCtx, chatCloseCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := chatMgr.Close(chatCloseCtx); err != nil {
			slog.Warn("chat manager close failed", "error", err)
		}

		chatCloseCancel()
	}

	return chatMgr, chatHub, cleanup, chatWorkerAPIKey
}
