package api

import (
	"context"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/backend"
)

// TaskBackend is the task-execution lifecycle channel CM drives:
// trigger/kill/message/promote.
// backend.Client - the webhook client for the agent backend - is the sole
// implementation. Card progress and usage reporting are NOT part of this
// surface - in-container agents report via CM's MCP tools.
type TaskBackend interface {
	Trigger(ctx context.Context, p protocol.TriggerPayload) error
	Kill(ctx context.Context, p protocol.KillPayload) error
	StopAll(ctx context.Context, p protocol.StopAllPayload) error
	Message(ctx context.Context, p protocol.MessagePayload) error
	Promote(ctx context.Context, p protocol.PromotePayload) error
	Health(ctx context.Context) (protocol.HealthResponse, error)
	ListImages(ctx context.Context) ([]backend.ImageInfo, error)
}

// Compile-time check: the backend webhook client implements the contract.
var _ TaskBackend = (*backend.Client)(nil)
