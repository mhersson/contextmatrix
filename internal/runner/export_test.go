package runner

import (
	"context"
	"log/slog"
)

// RunChatReconcileSweepForTest is a test-only export of
// runChatReconcileSweep, allowing package runner_test to exercise the
// standalone chat-reconcile entrypoint without accessing the unexported
// function directly.
func RunChatReconcileSweepForTest(ctx context.Context, chatMgr ChatReconciler, client ContainerLister, logger *slog.Logger) {
	runChatReconcileSweep(ctx, chatMgr, client, logger)
}
