package api

import (
	"context"

	"github.com/mhersson/contextmatrix/internal/runner"
)

// TaskBackend is the task-execution lifecycle channel CM drives:
// trigger/kill/message/promote/end-session plus container introspection.
// contextmatrix-runner's runner.Client is the sole implementation until the
// agent backend lands. Card progress and usage reporting are NOT part of
// this surface — in-container agents report via CM's MCP tools.
//
// Payload types are aliases of contextmatrix-protocol DTOs, so the
// interface is protocol-shaped. HealthInfo and ContainerInfo are parsed
// response shapes owned by internal/runner, not protocol DTOs.
type TaskBackend interface {
	Trigger(ctx context.Context, p runner.TriggerPayload) error
	Kill(ctx context.Context, p runner.KillPayload) error
	StopAll(ctx context.Context, p runner.StopAllPayload) error
	Message(ctx context.Context, p runner.MessagePayload) error
	Promote(ctx context.Context, p runner.PromotePayload) error
	EndSession(ctx context.Context, p runner.EndSessionPayload) error
	Health(ctx context.Context) (runner.HealthInfo, error)
	ListContainers(ctx context.Context) ([]runner.ContainerInfo, error)
}

// Compile-time check: the contextmatrix-runner webhook client implements
// the contract.
var _ TaskBackend = (*runner.Client)(nil)

// KnowledgeRefresher triggers a knowledge-base refresh container. Kept off
// TaskBackend deliberately: the KB machinery is scheduled for retirement,
// and only the contextmatrix-runner client implements it.
type KnowledgeRefresher interface {
	RefreshKnowledge(ctx context.Context, p runner.RefreshKnowledgePayload) error
}

var _ KnowledgeRefresher = (*runner.Client)(nil)
