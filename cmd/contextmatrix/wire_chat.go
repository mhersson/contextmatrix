package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/api"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/service"
)

// wireChat builds the chat subsystem: backend client, SSE hub, manager, idle
// reaper, warm-idle grace timers, and the startup reattach loop. The chat
// store is the shared operational store (ops.db), opened and owned by the
// caller - wireChat does not open or close it. Returns the manager, the hub,
// a cleanup function the caller must defer, and the resolved chat-backend
// api_key (empty when no chat backend is configured) - the caller wires this
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
	// slug). Empty when no chat backend is configured - sends fail at open
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
		// stub that errors on every operation - chat features require a
		// configured chat backend.
		chatBackend = chatBackendDisabled{}
	}

	// chatWorkerAPIKey is the secret behind WorkerCredentialsToken: empty
	// unless a chat backend is configured AND carries an api_key (see
	// RouterConfig.ChatWorkerAPIKey).
	chatWorkerAPIKey := ""

	var workerCredentialsToken func(sessionID string) string

	if chatBackendOK && chatBackendEntry.APIKey != "" {
		chatWorkerAPIKey = chatBackendEntry.APIKey
		workerCredentialsToken = func(sessionID string) string {
			return api.WorkerCredentialsToken(chatWorkerAPIKey, sessionID)
		}
	}

	// The architect entry speaks the chat protocol, so it gets the same
	// client; nil leaves the kind unavailable and CreateSession refuses it.
	var (
		architectBackend                chat.Backend
		architectDefaultModel           string
		architectWorkerCredentialsToken func(sessionID string) string
		research                        *protocol.ResearchStart
	)

	if entry, ok := cfg.ArchitectBackend(); ok {
		architectBackend = chat.NewBackendClient(chat.BackendClientConfig{
			BaseURL:   entry.URL,
			HMACKey:   entry.APIKey,
			MCPAPIKey: cfg.MCPAPIKey,
		})
		architectDefaultModel = entry.DefaultModel
		research = architectResearch(entry)

		// GET /api/worker/git-credentials verifies these against the same architect entry's api_key.
		architectKey := entry.APIKey
		architectWorkerCredentialsToken = func(sessionID string) string {
			return api.WorkerCredentialsToken(architectKey, sessionID)
		}
	}

	chatHub := chat.NewSSEHub(128)

	chatMgr := chat.NewManager(chat.Config{
		Store:                           chatStore,
		Backend:                         chatBackend,
		Clock:                           clock.Real(),
		IdleTTL:                         cfg.Chat.IdleTTL,
		MaxConcurrent:                   cfg.Chat.MaxConcurrent,
		Hub:                             chatHub,
		ResumeBudgetTokens:              cfg.Chat.ResumeBudgetTokens,
		RehydrationTimeout:              cfg.Chat.RehydrationTimeout,
		DefaultModel:                    chatDefaultModel,
		Pricer:                          svc,
		LLMEndpoint:                     llmEndpointFromConfig(cfg.LLMEndpoint),
		WorkerCredentialsToken:          workerCredentialsToken,
		ArchitectBackend:                architectBackend,
		ArchitectDefaultModel:           architectDefaultModel,
		ArchitectWorkerCredentialsToken: architectWorkerCredentialsToken,
		ArchitectPrice:                  architectPriceFor(svc),
		ArchitectResearch:               research,
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

			info.WorkerImage = chatWorkerImageFor(p)

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
			// arrived during the grace window - proceed with warm-idle.
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
		// /logs for this session - covers the case where CM restarted
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
	// up. Reattach is idempotent and tolerant of dead containers - a chat
	// consumer retries with backoff and stops on a clean close, an architect
	// consumer reconnects and re-sends the start; the reconcile sweep below
	// flips orphaned sessions to cold.
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

// chatWorkerImageFor returns the chat worker image for a project:
// remote_execution.chat_worker_image. worker_image deliberately does NOT flow
// to chat - the task and chat image families bake different worker
// entrypoints and are not interchangeable.
func chatWorkerImageFor(p *board.ProjectConfig) string {
	if p.RemoteExecution == nil {
		return ""
	}

	return p.RemoteExecution.ChatWorkerImage
}

// rateResolver is the narrow slice of *service.CardService that
// architectPriceFor needs.
type rateResolver interface {
	ResolvedRate(model string) (service.ModelRate, bool)
}

// architectPriceFor builds chat.Config.ArchitectPrice over the card service's
// resolved rates. An unpriceable rate reports no price: sent as zero, a
// capped blueprint would run under a cap that can never bind.
func architectPriceFor(rates rateResolver) func(model string) (protocol.ModelPrice, bool) {
	return func(model string) (protocol.ModelPrice, bool) {
		rate, ok := rates.ResolvedRate(model)
		if !ok || !priceable(rate) {
			return protocol.ModelPrice{}, false
		}

		return modelPriceFromRate(rate), true
	}
}

// priceable reports whether a rate prices prompt or completion tokens; a
// catalog entry without pricing resolves to an all-zero rate.
func priceable(rate service.ModelRate) bool {
	return rate.Prompt > 0 || rate.Completion > 0
}

// modelPriceFromRate converts a rate in USD per token to the wire's USD per
// million tokens.
func modelPriceFromRate(rate service.ModelRate) protocol.ModelPrice {
	const perMTok = 1e6

	return protocol.ModelPrice{
		PromptUSDPerMTok:     rate.Prompt * perMTok,
		CompletionUSDPerMTok: rate.Completion * perMTok,
		CacheReadUSDPerMTok:  rate.CacheRead * perMTok,
		CacheWriteUSDPerMTok: rate.CacheWrite * perMTok,
	}
}

// architectResearch is the research block of the architect start payload;
// nil keeps research off.
func architectResearch(entry *config.ArchitectBackendConfig) *protocol.ResearchStart {
	if entry.ResearchLevel() != config.ResearchSearch {
		return nil
	}

	return &protocol.ResearchStart{Provider: entry.ResearchProviderName(), APIKey: entry.ResearchAPIKey}
}
