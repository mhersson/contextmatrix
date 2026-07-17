package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/api"
	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	ghimport "github.com/mhersson/contextmatrix/internal/github"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/images"
	"github.com/mhersson/contextmatrix/internal/lock"
	mcpserver "github.com/mhersson/contextmatrix/internal/mcp"
	"github.com/mhersson/contextmatrix/internal/metrics"
	"github.com/mhersson/contextmatrix/internal/modelcatalog"
	opsqlite "github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/mhersson/contextmatrix/web"
)

var (
	version   string
	gitCommit string
	buildTime string
)

func buildVersion() string {
	if buildTime == "" {
		return ""
	}

	if version != "" {
		return buildTime + " " + version
	}

	if gitCommit != "" {
		return buildTime + " " + gitCommit
	}

	return buildTime
}

// llmEndpointFromConfig converts the on-disk llm_endpoint config into the
// wire-shape *protocol.LLMEndpoint carried on trigger/chat-start payloads.
// Nil when unconfigured (Type == "") so downstream consumers can treat
// "no endpoint" and "empty struct" identically. APIKey rides along on the
// returned value - never log it; log Type/BaseURL only if logging at all.
func llmEndpointFromConfig(e config.LLMEndpointConfig) *protocol.LLMEndpoint {
	if e.Type == "" {
		return nil
	}

	return &protocol.LLMEndpoint{
		Type:    e.Type,
		BaseURL: e.BaseURL,
		APIKey:  e.APIKey,
	}
}

func main() {
	// `contextmatrix auth <subcommand>` is a set of operator escape hatches
	// (admin recovery, master-key rotation) that run to completion and exit
	// without starting the server. Any other invocation falls through to the
	// unchanged server path below.
	if len(os.Args) > 1 && os.Args[1] == "auth" {
		os.Exit(runAuthCLI(os.Args[2:]))
	}

	configPath := flag.String("config", "", "path to config file")

	flag.Parse()

	if *configPath == "" {
		found := config.FindConfigPath()
		if found == "" {
			slog.New(slog.NewTextHandler(os.Stdout, nil)).Error(
				"no config file found; use -config to specify a path",
			)
			os.Exit(1)
		}

		configPath = &found
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		// Use a plain text handler for the startup error since config isn't loaded yet.
		slog.New(slog.NewTextHandler(os.Stdout, nil)).Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Register Prometheus metrics early, before building any component that
	// may observe them.
	metrics.Register(prometheus.DefaultRegisterer)

	logger := slog.New(cfg.BuildSlogHandler(os.Stdout))
	slog.SetDefault(logger)

	slog.Info("config loaded", "path", *configPath)

	// Construct the GitHub token provider (used by boards git, task-skills
	// git, REST API for issue importing, REST API for branch listing).
	var inner githubauth.TokenGenerator

	switch cfg.GitHub.AuthMode {
	case "app":
		inner, err = githubauth.NewAppProvider(
			cfg.GitHub.App.AppID,
			cfg.GitHub.App.InstallationID,
			cfg.GitHub.App.PrivateKeyPath,
			githubauth.WithAPIBaseURL(cfg.GitHub.ResolvedAPIBaseURL()),
		)
	case "pat":
		inner, err = githubauth.NewPATProvider(cfg.GitHub.PAT.Token)
	default:
		err = fmt.Errorf("unreachable: invalid auth_mode %q", cfg.GitHub.AuthMode)
	}

	if err != nil {
		slog.Error("failed to construct github token provider", "error", err)
		os.Exit(1)
	}

	tokenProvider := githubauth.NewCachingProvider(inner)

	slog.Info("github token provider initialized", "auth_mode", cfg.GitHub.AuthMode)

	heartbeatTimeout, err := cfg.HeartbeatDuration()
	if err != nil {
		slog.Error("invalid heartbeat_timeout", "error", err)
		os.Exit(1)
	}

	// Initialize git manager (boards directory IS the git repo).
	// Must run before storage so clone-on-empty can populate the directory.
	boardsCloneURL := ""
	if cfg.Boards.GitCloneOnEmpty {
		boardsCloneURL = cfg.Boards.GitRemoteURL
	}

	git, err := gitops.NewManager(cfg.Boards.Dir, boardsCloneURL, "boards", tokenProvider)
	if err != nil {
		slog.Error("failed to create boards git manager", "error", err)
		os.Exit(1)
	}

	slog.Info("boards git manager initialized", "repo_path", cfg.Boards.Dir)

	// Initialize task-skills git manager.
	taskSkillsCloneURL := ""
	if cfg.TaskSkills.GitCloneOnEmpty {
		taskSkillsCloneURL = cfg.TaskSkills.GitRemoteURL
	}

	// Capture whether the task-skills dir already has a .git before NewManager
	// runs PlainInit on an empty directory.
	taskSkillsHadGit := dirHasGit(cfg.TaskSkills.Dir)

	taskSkillsGit, err := gitops.NewManager(
		cfg.TaskSkills.Dir,
		taskSkillsCloneURL,
		"task-skills",
		tokenProvider,
	)
	if err != nil {
		slog.Error("failed to create task-skills git manager", "error", err)
		os.Exit(1)
	}

	slog.Info("task-skills git manager initialized", "repo_path", cfg.TaskSkills.Dir)

	startupPullTaskSkills(taskSkillsHadGit, cfg.TaskSkills.GitRemoteURL, taskSkillsGit)

	// Initialize storage
	store, err := storage.NewFilesystemStore(cfg.Boards.Dir)
	if err != nil {
		slog.Error("failed to create storage", "error", err)
		os.Exit(1)
	}

	slog.Info("storage initialized", "boards_dir", cfg.Boards.Dir)

	// Initialize event bus
	bus := events.NewBus()

	slog.Info("event bus initialized")

	// Initialize lock manager
	lockMgr := lock.NewManager(store, heartbeatTimeout)
	slog.Info("lock manager initialized", "timeout", heartbeatTimeout)

	// Convert token costs from config to service types
	var tokenCosts map[string]service.ModelRate
	if cfg.TokenCosts != nil {
		tokenCosts = make(map[string]service.ModelRate, len(cfg.TokenCosts))
		for model, cost := range cfg.TokenCosts {
			tokenCosts[model] = service.ModelRate{Prompt: cost.Prompt, Completion: cost.Completion}
		}
	}

	// Initialize card service
	svc := service.NewCardService(store, git, lockMgr, bus, cfg.Boards.Dir, tokenCosts, cfg.Boards.GitAutoCommit, cfg.Boards.GitDeferredCommit)

	slog.Info("card service initialized")

	// Initialize the per-project commit queue so writes do not serialize on
	// the blocking go-git call under writeMu. A 30-minute idle timeout
	// tears down workers for quiet projects so long-running servers with
	// ephemeral projects do not accumulate goroutines; the next Enqueue
	// for that project spawns a fresh worker transparently.
	commitQueue := gitops.NewCommitQueue(git, 0, gitops.WithIdleTimeout(30*time.Minute))
	svc.SetCommitQueue(commitQueue)
	slog.Info("commit queue initialized")

	// Create context for background tasks
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// httpCtx is cancelled at the start of shutdown so that long-lived
	// connections (SSE streams) exit immediately instead of holding
	// server.Shutdown hostage until the timeout expires.
	httpCtx, httpCancel := context.WithCancel(ctx)
	defer httpCancel()

	// Start timeout checker. Interval is configurable so test harnesses
	// can shrink it for fast heartbeat-timeout scenarios; production
	// default is 1m. Validate ensures the duration parses and is positive.
	stalledTick, err := cfg.StalledCheckIntervalDuration()
	if err != nil {
		slog.Error("invalid stalled_check_interval; falling back to 1m", "error", err)

		stalledTick = time.Minute
	}

	svc.StartTimeoutChecker(ctx, stalledTick)

	// Initialize git sync
	syncer := wireGitSync(ctx, cfg, git, store, svc, bus)

	// Multi-user auth: master key, auth.db, service, bootstrap link, janitor.
	// In auth.mode "none" every one of these stays nil/off and the router
	// behaves exactly as single-user CM always has.
	//
	// Declared and resolved before the GitHub issue syncer below so that
	// providerForProject (which closes over authSvc) never races the
	// syncer's background goroutine: authSvc is fully set - nil or not -
	// before ghSyncer.Start(ctx) ever runs.
	var authSvc *auth.Service

	if cfg.Auth.Mode == config.AuthModeMulti {
		masterKey, keyCreated, err := auth.LoadOrCreateMasterKey(cfg.Auth.MasterKeyFile)
		if err != nil {
			slog.Error("failed to load auth master key", "path", cfg.Auth.MasterKeyFile, "error", err)
			cancel()
			os.Exit(1) //nolint:gocritic // cancel called explicitly above
		}

		if keyCreated {
			slog.Warn("auth: master key AUTO-GENERATED - move this file into real secret management",
				"path", cfg.Auth.MasterKeyFile)
		}

		authStore, err := authstore.Open(cfg.Auth.DBPath)
		if err != nil {
			slog.Error("failed to open auth store", "path", cfg.Auth.DBPath, "error", err)
			cancel()
			os.Exit(1) //nolint:gocritic // cancel called explicitly above
		}
		defer authStore.Close()

		slog.Info("auth store opened", "path", cfg.Auth.DBPath)

		idleTTL, err := cfg.SessionIdleTTLDuration()
		if err != nil {
			// Unreachable: Validate() parses it. Belt and suspenders.
			slog.Error("invalid auth.session_idle_ttl", "error", err)
			cancel()
			os.Exit(1) //nolint:gocritic // cancel called explicitly above
		}

		authSvc = auth.NewService(authStore, idleTTL)

		credKey, err := auth.DeriveKey(masterKey, auth.KeyPurposeCredentials)
		if err != nil {
			slog.Error("failed to derive credential key", "error", err)
			cancel()
			os.Exit(1) //nolint:gocritic // cancel called explicitly above
		}

		authSvc.SetCredentialKey(credKey)

		// First-start bootstrap: with zero users nobody can log in, so mint
		// a one-time admin-creation link and print it. Re-issued on every
		// zero-user start; redemption re-checks the zero-user invariant.
		users, err := authStore.ListUsers(ctx)
		if err != nil {
			slog.Error("failed to check for existing users", "error", err)
			cancel()
			os.Exit(1) //nolint:gocritic // cancel called explicitly above
		}

		if len(users) == 0 {
			bootstrapToken, err := authSvc.IssueBootstrapToken(ctx)
			if err != nil {
				slog.Error("failed to issue bootstrap token", "error", err)
				cancel()
				os.Exit(1) //nolint:gocritic // cancel called explicitly above
			}

			slog.Info("=======================================================================")
			slog.Info("auth: no users exist yet - create the first admin account by opening:")
			slog.Info("auth: bootstrap link", "path", "/auth/token/"+bootstrapToken)
			slog.Info("auth: (prefix with this server's URL; the link is valid for 48h)")
			slog.Info("=======================================================================")
		}

		// Janitor: hourly sweep of expired sessions and unused expired
		// one-time tokens.
		go func() {
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if n, err := authStore.DeleteExpiredSessions(ctx, time.Now()); err == nil && n > 0 {
						slog.Debug("auth janitor: swept expired sessions", "count", n)
					}

					if n, err := authStore.DeleteExpiredOneTimeTokens(ctx, time.Now()); err == nil && n > 0 {
						slog.Debug("auth janitor: swept expired tokens", "count", n)
					}
				}
			}
		}()

		slog.Info("multi-user auth enabled", "session_idle_ttl", idleTTL)
	}

	// ghAPIBase is the instance-wide GitHub API base URL - the fallback used
	// by providerForProject whenever a project has no credential binding, or
	// the instance runs in auth.mode "none".
	ghAPIBase := cfg.GitHub.ResolvedAPIBaseURL()

	// providerForProject resolves the token provider for a project's GitHub
	// operations. Used by both branch listing and GitHub issue sync below.
	// authSvc is fully resolved by this point (see comment above), so this
	// closure is race-free even though the issue syncer's background
	// goroutine may invoke it concurrently with later startup code. See
	// newProviderForProject (provider.go) for the resolution logic and its
	// direct test coverage.
	providerForProject := newProviderForProject(svc, authSvc, tokenProvider, ghAPIBase)

	// Start GitHub issue syncer if configured
	var ghSyncer *ghimport.Syncer

	if cfg.GitHub.IssueImporting.Enabled {
		syncInterval, _ := cfg.GitHub.IssueImporting.SyncIntervalDuration()
		ghClient := ghimport.NewClientWithBaseURL(tokenProvider, ghAPIBase)
		ghSyncer = ghimport.NewSyncer(svc, store, ghClient, cfg.Boards.Dir, syncInterval, cfg.GitHub.AllowedHosts())

		// Credential bindings only exist when auth is enabled - .board.yaml
		// bindings are validated against authSvc's credential pool, which is
		// nil in auth.mode "none". Leave clientFor unset (nil seam) there so
		// sync cycles keep using the constructor-injected static client.
		if authSvc != nil {
			ghSyncer.SetClientFor(func(ctx context.Context, pcfg *board.ProjectConfig) (*ghimport.Client, error) {
				provider, apiBase, err := providerForProject(ctx, pcfg.Name)
				if err != nil {
					return nil, err
				}

				return ghimport.NewClientWithBaseURL(provider, apiBase), nil
			})
		}

		ghSyncer.Start(ctx)
		slog.Info("github issue sync enabled", "interval", syncInterval)
	}

	// Images: SQLite blob store for paste/drop screenshot uploads.
	imageStore, err := images.Open(cfg.Images.DBPath)
	if err != nil {
		slog.Error("failed to open image store", "path", cfg.Images.DBPath, "error", err)
		cancel()
		os.Exit(1) //nolint:gocritic // cancel called explicitly above
	}
	defer imageStore.Close()

	slog.Info("image store opened", "path", cfg.Images.DBPath)

	// Op store: shared operational SQLite DB. Holds the chat schema (sessions,
	// messages, cost archive) and the model blacklist in one ops.db - the chat
	// manager, MCP report_incapable_model, and the runCard blacklist reader all
	// use this single store.
	opStore, err := opsqlite.Open(cfg.OpStore.DBPath)
	if err != nil {
		slog.Error("failed to open op store", "path", cfg.OpStore.DBPath, "error", err)
		cancel()
		os.Exit(1) //nolint:gocritic // cancel called explicitly above
	}
	defer opStore.Close()

	slog.Info("op store opened", "path", cfg.OpStore.DBPath)

	// Model catalog builder: constructed whenever there is a rate source. The
	// AA+agent path also yields selection candidates (routerCfg.Catalog below);
	// the endpoint-only path (chat-only deployments) yields pricing only.
	var catalogBuilder *modelcatalog.Builder

	// agentCfg is nil when the agent backend is absent or disabled; every
	// read below is behind hasAgent.
	agentCfg, hasAgent := cfg.AgentBackend()
	agentAA := hasAgent && agentCfg.AAAPIKey != ""

	chatBackendCfg, chatBackendEnabled := cfg.ChatBackend()
	chatOpenRouter := chatBackendEnabled && chatBackendCfg.APIKey != ""

	switch {
	case agentAA:
		var opts []modelcatalog.BuilderOption

		if cfg.LLMEndpoint.Type == config.LLMEndpointTypeOpenAI {
			priors := make(map[string]modelcatalog.PriorOverride, len(agentCfg.ModelPriors))
			for slug, p := range agentCfg.ModelPriors {
				priors[slug] = modelcatalog.PriorOverride{Coder: p.Coder, Reviewer: p.Reviewer}
			}

			opts = append(opts, modelcatalog.WithEndpoint(
				cfg.LLMEndpoint.BaseURL, cfg.LLMEndpoint.APIKey, agentCfg.AAModelMap, priors))
		}

		opts = append(opts, modelcatalog.WithFavorites(flattenFavorites(agentCfg.Favorites)))

		catalogBuilder = modelcatalog.NewBuilder(agentCfg.AAAPIKey, 0.65, agentCfg.ModelAllowlist, 0, opts...)

		slog.Info("model catalog builder initialized", "endpoint_type", cfg.LLMEndpoint.Type, "mode", "aa+candidates")

	case cfg.LLMEndpoint.Type == config.LLMEndpointTypeOpenAI:
		// Endpoint pricing without AA/agent: Rate() prices endpoint-served models
		// for chat cost accounting; there are no selection candidates.
		catalogBuilder = modelcatalog.NewBuilder("", 0.65, nil, 0,
			modelcatalog.WithEndpoint(cfg.LLMEndpoint.BaseURL, cfg.LLMEndpoint.APIKey, nil, nil))

		slog.Info("model catalog builder initialized", "endpoint_type", cfg.LLMEndpoint.Type, "mode", "endpoint-pricing-only")

	case hasAgent || chatOpenRouter:
		// OpenRouter catalog without AA: no selection candidates, but the
		// served set drives the model pickers, write-time validation, and
		// chat cost pricing on AA-less deployments. On a chat-only deployment
		// there is no agent entry - no allowlist or favorites apply.
		var (
			allowlist []string
			favorites map[string]board.TierFavorites
		)

		if hasAgent {
			allowlist = agentCfg.ModelAllowlist
			favorites = agentCfg.Favorites
		}

		catalogBuilder = modelcatalog.NewBuilder("", 0.65, allowlist, 0,
			modelcatalog.WithFavorites(flattenFavorites(favorites)))

		slog.Info("model catalog builder initialized", "endpoint_type", cfg.LLMEndpoint.Type, "mode", "openrouter-catalog-only")
	}

	// Wire catalog rate lookup into the service so every cost path (ReportUsage,
	// RecalculateCosts, PriceTokens) can price models that are served but not in
	// the static token_costs override map (e.g. the agent's primary model when
	// no explicit rate is configured).
	var (
		servedModelsFn      func(context.Context) []api.ServedModelView
		validateChatModelFn func(context.Context, string) bool
		servedModelsSource  string
	)

	if catalogBuilder != nil {
		svc.SetCatalogRateLookup(func(model string) (service.ModelRate, bool) {
			p, c, ok := catalogBuilder.Rate(ctx, model)
			if !ok {
				return service.ModelRate{}, false
			}

			return service.ModelRate{Prompt: p, Completion: c}, true
		})

		svc.SetModelValidator(catalogBuilder.Validate)

		routerServedModels := func(ctx context.Context) []api.ServedModelView {
			served := catalogBuilder.Served(ctx)
			views := make([]api.ServedModelView, len(served))

			for i, m := range served {
				views[i] = api.ServedModelView{ID: m.Slug, ContextWindow: m.ContextWindow}
			}

			return views
		}

		servedModelsFn = routerServedModels
		validateChatModelFn = catalogBuilder.Validate
		servedModelsSource = "openrouter"

		if cfg.LLMEndpoint.Type == config.LLMEndpointTypeOpenAI {
			servedModelsSource = "endpoint"
		}
	}

	// Chat: manager + SSE hub + idle reaper + warm-idle grace timer. The chat
	// store is the shared operational store (opStore) opened above.
	chatMgr, chatHub, chatCleanup, chatWorkerAPIKey := wireChat(ctx, cfg, svc, opStore)
	defer chatCleanup()

	// Wire backend subsystems: client, end-session subscriber, reconcile sweep,
	// and session-log manager.
	backendSys := wireBackendSubsystems(ctx, cfg, svc, bus)

	// Interface fields must stay untyped-nil when the backend is disabled -
	// a nil *backend.Client wrapped in the interface would defeat every
	// `!= nil` enablement check in the router.
	var taskBackend api.TaskBackend

	if backendSys.Client != nil {
		taskBackend = backendSys.Client
	}

	// Attribute backend-generated audit-trail entries to the agent backend
	// when it resolves. Left unset otherwise - the affected service paths are
	// unreachable then, and the neutral "backend" default applies.
	if hasAgent {
		svc.SetTaskBackendName(config.BackendNameAgent)
	}

	sessionMgr := backendSys.SessionLog

	// Create MCP server
	mcpSrv := mcpserver.NewServer(mcpserver.ServerConfig{
		Service:           svc,
		WorkflowSkillsDir: cfg.WorkflowSkillsDir,
		ChatManager:       chatMgr,
		ImageStore:        imageStore,
		Blacklist:         opStore,
		Outcomes:          opStore,
	})

	mcpHandler := mcpserver.NewHandler(mcpSrv, cfg.MCPAPIKey)
	if cfg.MCPAPIKey != "" {
		slog.Info("MCP authentication enabled")
	}

	// Create router with all API routes. MCP is registered on the inner mux
	// so it shares the same middleware chain as every other route - no
	// separate wrapping needed here.
	var apiSyncer api.Syncer
	if syncer != nil {
		apiSyncer = syncer
	}

	// Build RouterConfig with fields that are always present. Catalog is set
	// conditionally below to avoid boxing a nil *modelcatalog.Builder into the
	// catalogProvider interface - a typed nil defeats the h.catalog != nil guard
	// in runCard and causes a panic on the mutex lock (nil receiver dereference).
	// Blacklist, Outcomes, and OutcomesAdmin (all opStore) are always
	// non-nil so they are set unconditionally.
	// chatBackendCfg is the dedicated "chat" backend entry (nil when absent
	// or disabled), already fetched above for the catalog builder switch. Its
	// key authenticates the chat service's task-skills pointer fetch.
	routerCfg := api.RouterConfig{
		Service:                svc,
		Bus:                    bus,
		CORSOrigin:             cfg.CORSOrigin,
		Syncer:                 apiSyncer,
		Backend:                taskBackend,
		AgentBackendCfg:        agentCfg,
		MCPAPIKey:              cfg.MCPAPIKey,
		GitHubTokenProvider:    tokenProvider,
		TaskSkillsDir:          cfg.TaskSkills.Dir,
		TaskSkillsGitRemoteURL: cfg.TaskSkills.GitRemoteURL,
		GitHubAPIBaseURL:       ghAPIBase,
		GitHubAllowedHosts:     cfg.GitHub.AllowedHosts(),
		ProviderForProject:     providerForProject,
		SessionManager:         sessionMgr,
		Theme:                  cfg.Theme,
		Version:                buildVersion(),
		MCPHandler:             mcpHandler,
		ChatManager:            chatMgr,
		ChatHub:                chatHub,
		ChatBackendCfg:         chatBackendCfg,
		ChatWorkerAPIKey:       chatWorkerAPIKey,
		ImageStore:             imageStore,
		Blacklist:              opStore,
		Outcomes:               opStore,
		OutcomesAdmin:          opStore,
		ServedModels:           servedModelsFn,      // nil when catalogBuilder == nil
		ServedModelsSource:     servedModelsSource,  // "" when catalogBuilder == nil
		ValidateChatModel:      validateChatModelFn, // nil when catalogBuilder == nil
		AuthService:            authSvc,
		AuthMode:               cfg.Auth.Mode,
		LLMEndpoint:            llmEndpointFromConfig(cfg.LLMEndpoint),
		BestOfN:                cfg.BestOfN,
		Mob:                    cfg.Mob,
	}
	if catalogBuilder != nil && agentAA {
		routerCfg.Catalog = catalogBuilder
	}

	if authSvc != nil {
		routerCfg.CredentialExists = authSvc.CredentialExists
	}

	if cfg.LLMEndpoint.Type == config.LLMEndpointTypeOpenAI {
		toViews := func(eps []modelcatalog.EndpointModel) []api.EndpointModelView {
			out := make([]api.EndpointModelView, len(eps))

			for i, e := range eps {
				out[i] = api.EndpointModelView{ID: e.ID, Label: e.Label, MaxTokens: e.MaxTokens}
			}

			return out
		}

		if catalogBuilder != nil {
			// Serve the picker from the Builder's cached catalog - the same
			// /models fetch already shared by Rate and Candidates - instead of a
			// second independent fetch with its own TTL.
			routerCfg.ChatEndpointModels = func(ctx context.Context) ([]api.EndpointModelView, error) {
				return toViews(catalogBuilder.EndpointModels(ctx)), nil
			}
		} else {
			baseURL := cfg.LLMEndpoint.BaseURL
			apiKey := cfg.LLMEndpoint.APIKey
			routerCfg.ChatEndpointModels = func(ctx context.Context) ([]api.EndpointModelView, error) {
				eps, err := modelcatalog.FetchEndpointModels(ctx, baseURL, apiKey)
				if err != nil {
					return nil, err
				}

				return toViews(eps), nil
			}
		}
	}

	mux := api.NewRouter(routerCfg)

	slog.Info("MCP server registered", "endpoint", "/mcp")

	// Embed frontend and create SPA handler
	distFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		slog.Error("failed to create dist filesystem", "error", err)
		cancel()
		os.Exit(1) //nolint:gocritic // cancel called explicitly above
	}

	handler := newSPAHandler(mux, distFS)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       func(_ net.Listener) context.Context { return httpCtx },
	}

	errCh := make(chan error, 2)

	go func() {
		slog.Info("starting server", "port", cfg.Port)

		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)

			errCh <- err
		}
	}()

	var adminServer *http.Server

	if cfg.AdminPort > 0 {
		adminBind := cfg.AdminBindAddr
		if adminBind == "" {
			adminBind = "127.0.0.1"
		}

		if adminBind != "127.0.0.1" && adminBind != "localhost" && adminBind != "::1" {
			slog.Warn("admin server bound to non-loopback address - pprof/metrics exposed; restrict via firewall",
				"addr", adminBind, "port", cfg.AdminPort)
		}

		adminServer = &http.Server{
			Addr:              net.JoinHostPort(adminBind, strconv.Itoa(cfg.AdminPort)),
			Handler:           newAdminMux(),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
		}

		go func() {
			slog.Info("starting admin server", "addr", adminServer.Addr)

			if err := adminServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("admin server error", "error", err)

				errCh <- err
			}
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
	case err := <-errCh:
		slog.Error("server failed, initiating shutdown", "error", err)
	}

	slog.Info("shutdown: initiated")

	// 5s is enough for in-flight REST requests to complete. Long-lived SSE
	// connections are already terminated by httpCancel() before Shutdown is
	// called.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := runShutdownSequence(shutdownCtx, shutdownComponents{
		HTTPServer:  server,
		AdminServer: adminServer,
		SessionLog:  sessionMgr,
		CommitQueue: commitQueue,
		Syncer:      syncer,
		GHSyncer:    ghSyncer,
		HTTPCancel:  httpCancel,
		AppCancel:   cancel,
	}); err != nil {
		slog.Error("server shutdown error", "error", err)
		os.Exit(1)
	}
}

// dirHasGit reports whether <dir>/.git exists (as a file or directory).
// Returns false for an empty dir string.
func dirHasGit(dir string) bool {
	if dir == "" {
		return false
	}

	_, err := os.Stat(filepath.Join(dir, ".git"))

	return err == nil
}

// flattenFavorites de-duplicates every favorite slug across tiers and roles
// for the catalog Builder's vendor screen.
func flattenFavorites(favs map[string]board.TierFavorites) []string {
	seen := map[string]bool{}

	var out []string

	add := func(slugs []string) {
		for _, s := range slugs {
			if !seen[s] {
				seen[s] = true

				out = append(out, s)
			}
		}
	}

	for _, tf := range favs {
		add(tf.All)

		for _, slugs := range tf.ByRole {
			add(slugs)
		}
	}

	sort.Strings(out)

	return out
}

// startupPullTaskSkills performs a fast-forward pull of the task-skills repo
// at server startup. It is a best-effort operation: pull failures are logged
// as warnings but do not prevent the server from starting.
func startupPullTaskSkills(hadGit bool, remoteURL string, mgr *gitops.Manager) {
	if !hadGit || remoteURL == "" || mgr == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := mgr.PullFastForward(ctx); err != nil {
		slog.Warn("task-skills startup pull failed; serving cached copy",
			"dir", mgr.RepoPath(), "error", err)

		return
	}

	slog.Info("task-skills startup pull: ok", "dir", mgr.RepoPath())
}

// waitSyncer wraps a blocking Wait() call with a context deadline. It runs
// Wait() in a goroutine and returns nil as soon as it returns, or ctx.Err()
// if the deadline fires first. The goroutine is leaked in the timeout case -
// acceptable at shutdown because the process exits shortly after.
func waitSyncer(ctx context.Context, wait func()) error {
	done := make(chan struct{})

	go func() {
		defer close(done)

		wait()
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// newAdminMux returns a mux serving /debug/pprof/* and /metrics. The mux is
// intentionally scoped: it never mounts http.DefaultServeMux so nothing else
// that imports net/http/pprof (or blindly calls http.Handle) can leak through
// the admin listener.
func newAdminMux() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	mux.Handle("GET /metrics", promhttp.Handler())

	return mux
}

// newSPAHandler wraps the API handler with static file serving and SPA fallback.
// API and health check routes are forwarded to the API handler. All other requests
// are served from the embedded frontend filesystem, falling back to index.html
// for client-side routing.
func newSPAHandler(apiHandler http.Handler, fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/mcp" {
			apiHandler.ServeHTTP(w, r)

			return
		}

		// Admin endpoints live on a separate listener; serving the SPA shell
		// for them on the main port would be confusing. 404 explicitly so
		// operators scraping the wrong port get a clear signal.
		if r.URL.Path == "/metrics" || strings.HasPrefix(r.URL.Path, "/debug/pprof/") {
			http.NotFound(w, r)

			return
		}

		// Try to serve a static file from the embedded dist
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if _, err := fs.Stat(fsys, path); err == nil {
			fileServer.ServeHTTP(w, r)

			return
		}

		// SPA fallback: serve index.html for client-side routes
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

// chatBackendDisabled is a no-op chat.Backend used when no chat backend is
// configured. Every operation returns an error so callers fail fast instead
// of nil-panicking.
type chatBackendDisabled struct{}

func (chatBackendDisabled) StartChat(_ context.Context, _ chat.StartChatOpts) (string, error) {
	return "", fmt.Errorf("chat: no chat backend configured")
}

func (chatBackendDisabled) EndChat(_ context.Context, _ string) error {
	return fmt.Errorf("chat: no chat backend configured")
}

func (chatBackendDisabled) SendChatMessage(_ context.Context, _, _, _ string) error {
	return fmt.Errorf("chat: no chat backend configured")
}

func (chatBackendDisabled) StreamLogs(ctx context.Context, _ string, _ func(chat.LogEntry)) error {
	<-ctx.Done()

	return ctx.Err()
}
