// Package chat provides the global chat panel orchestration layer.
// It coordinates the SQLite-backed session/transcript store, the runner
// client for spawning chat containers, the idle TTL reaper, and the SSE
// fan-out for browser subscribers.
//
// File layout:
//   - doc.go         — package documentation.
//   - types.go       — Session, Message, Status enums.
//   - store.go       — Store interface used by Manager.
//   - sqlite/        — SQLite implementation of Store.
//   - manager.go     — Manager: session lifecycle orchestration.
//   - runner.go      — RunnerClient: wraps chat-mode runner webhooks.
//   - reaper.go      — IdleReaper: TTL goroutine.
//   - sse.go         — Per-session SSE buffer and fan-out hub.
package chat
