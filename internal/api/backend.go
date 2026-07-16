package api

import (
	"context"

	"github.com/mhersson/contextmatrix/internal/backend"
)

// TaskBackend is the task-execution lifecycle channel CM drives:
// trigger/kill/message/promote.
// backend.Client — the webhook client for the agent backend — is the sole
// implementation. Card progress and usage reporting are NOT part of this
// surface — in-container agents report via CM's MCP tools.
//
// Payload types are aliases of contextmatrix-protocol DTOs, so the
// interface is protocol-shaped. HealthInfo is a parsed
// response shape owned by internal/backend, not a protocol DTO.
type TaskBackend interface {
	Trigger(ctx context.Context, p backend.TriggerPayload) error
	Kill(ctx context.Context, p backend.KillPayload) error
	StopAll(ctx context.Context, p backend.StopAllPayload) error
	Message(ctx context.Context, p backend.MessagePayload) error
	Promote(ctx context.Context, p backend.PromotePayload) error
	Health(ctx context.Context) (backend.HealthInfo, error)
	ListImages(ctx context.Context) ([]backend.ImageInfo, error)
}

// Compile-time check: the backend webhook client implements the contract.
var _ TaskBackend = (*backend.Client)(nil)
