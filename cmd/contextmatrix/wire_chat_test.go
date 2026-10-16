package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

func TestChatWorkerImageFor_CleanCut(t *testing.T) {
	tests := []struct {
		name string
		re   *board.RemoteExecutionConfig
		want string
	}{
		{name: "no remote execution config", re: nil, want: ""},
		{
			name: "chat image set",
			re:   &board.RemoteExecutionConfig{ChatWorkerImage: "contextmatrix-chat-worker:go-node"},
			want: "contextmatrix-chat-worker:go-node",
		},
		{
			// The clean-cut pin: worker_image (task image) must never leak
			// into chat sessions - the image families are not interchangeable.
			name: "task image set but chat image empty",
			re:   &board.RemoteExecutionConfig{WorkerImage: "contextmatrix-agent-worker:go-node"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &board.ProjectConfig{RemoteExecution: tt.re}
			assert.Equal(t, tt.want, chatWorkerImageFor(p))
		})
	}
}

func TestModelPriceFromRate(t *testing.T) {
	tests := []struct {
		name                               string
		rate                               service.ModelRate
		prompt, completion, cacheR, cacheW float64
	}{
		{
			name:       "default cache multipliers",
			rate:       service.ModelRate{Prompt: 3e-6, Completion: 15e-6}.Resolved(),
			prompt:     3,
			completion: 15,
			cacheR:     0.3,
			cacheW:     3.75,
		},
		{
			name:       "explicit cache rates scale too",
			rate:       service.ModelRate{Prompt: 1e-6, Completion: 2e-6, CacheRead: 0.5e-6, CacheWrite: 1.5e-6},
			prompt:     1,
			completion: 2,
			cacheR:     0.5,
			cacheW:     1.5,
		},
		{name: "zero rate stays zero", rate: service.ModelRate{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := modelPriceFromRate(tt.rate)

			assert.InDelta(t, tt.prompt, got.PromptUSDPerMTok, 1e-9)
			assert.InDelta(t, tt.completion, got.CompletionUSDPerMTok, 1e-9)
			assert.InDelta(t, tt.cacheR, got.CacheReadUSDPerMTok, 1e-9)
			assert.InDelta(t, tt.cacheW, got.CacheWriteUSDPerMTok, 1e-9)
		})
	}
}
