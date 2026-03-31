package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mhersson/contextmatrix/internal/api"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	mcpserver "github.com/mhersson/contextmatrix/internal/mcp"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/mhersson/contextmatrix/web"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Parse heartbeat timeout
	heartbeatTimeout, err := cfg.HeartbeatDuration()
	if err != nil {
		slog.Error("invalid heartbeat_timeout", "error", err)
		os.Exit(1)
	}

	// Initialize storage
	store, err := storage.NewFilesystemStore(cfg.BoardsDir)
	if err != nil {
		slog.Error("failed to create storage", "error", err)
		os.Exit(1)
	}
	slog.Info("storage initialized", "boards_dir", cfg.BoardsDir)

	// Initialize git manager (boards directory IS the git repo)
	git, err := gitops.NewManager(cfg.BoardsDir)
	if err != nil {
		slog.Error("failed to create git manager", "error", err)
		os.Exit(1)
	}
	slog.Info("git manager initialized", "repo_path", cfg.BoardsDir)

	// Initialize event bus
	bus := events.NewBus()
	slog.Info("event bus initialized")

	// Initialize lock manager
	lockMgr := lock.NewManager(store, heartbeatTimeout)
	slog.Info("lock manager initialized", "timeout", heartbeatTimeout)

	// Initialize card service
	svc := service.NewCardService(store, git, lockMgr, bus, cfg.BoardsDir)
	slog.Info("card service initialized")

	// Create context for background tasks
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start timeout checker (checks every minute)
	svc.StartTimeoutChecker(ctx, time.Minute)

	// Create router with all API routes
	mux := api.NewRouter(svc, bus, cfg.CORSOrigin)

	// Create MCP server and register on the mux
	mcpSrv := mcpserver.NewServer(svc, cfg.SkillsDir)
	mcpHandler := mcpserver.NewHandler(mcpSrv)
	mux.Handle("POST /mcp", mcpHandler)
	mux.Handle("GET /mcp", mcpHandler)
	mux.Handle("DELETE /mcp", mcpHandler)
	slog.Info("MCP server registered", "endpoint", "/mcp")

	// Embed frontend and create SPA handler
	distFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		slog.Error("failed to create dist filesystem", "error", err)
		os.Exit(1)
	}
	handler := newSPAHandler(mux, distFS)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: handler,
	}

	go func() {
		slog.Info("starting server", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")

	// Stop background tasks
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}

// newSPAHandler wraps the API handler with static file serving and SPA fallback.
// API and health check routes are forwarded to the API handler. All other requests
// are served from the embedded frontend filesystem, falling back to index.html
// for client-side routing.
func newSPAHandler(apiHandler http.Handler, fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" || r.URL.Path == "/mcp" {
			apiHandler.ServeHTTP(w, r)
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
