package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix/internal/config"
)

func TestConfiguredBackendKinds(t *testing.T) {
	off := false

	tests := []struct {
		name  string
		build func() config.Backends
		want  []string
	}{
		{name: "none declared", build: func() config.Backends { return config.Backends{} }, want: nil},
		{
			name:  "agent only",
			build: func() config.Backends { return config.Backends{Agent: &config.AgentBackendConfig{}} },
			want:  []string{config.BackendNameAgent},
		},
		{
			name:  "chat only",
			build: func() config.Backends { return config.Backends{Chat: &config.ChatBackendConfig{}} },
			want:  []string{config.BackendNameChat},
		},
		{
			name:  "architect only",
			build: func() config.Backends { return config.Backends{Architect: &config.ArchitectBackendConfig{}} },
			want:  []string{config.BackendNameArchitect},
		},
		{
			name: "all enabled keep fixed order",
			build: func() config.Backends {
				return config.Backends{
					Agent:     &config.AgentBackendConfig{},
					Chat:      &config.ChatBackendConfig{},
					Architect: &config.ArchitectBackendConfig{},
				}
			},
			want: []string{config.BackendNameAgent, config.BackendNameChat, config.BackendNameArchitect},
		},
		{
			name: "declared but disabled are skipped",
			build: func() config.Backends {
				return config.Backends{
					Agent:     &config.AgentBackendConfig{Enabled: &off},
					Chat:      &config.ChatBackendConfig{},
					Architect: &config.ArchitectBackendConfig{Enabled: &off},
				}
			},
			want: []string{config.BackendNameChat},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Backends: tt.build()}
			assert.Equal(t, tt.want, configuredBackendKinds(cfg))
		})
	}
}
