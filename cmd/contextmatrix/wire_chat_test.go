package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/board"
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
			// into chat sessions — the image families are not interchangeable.
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
