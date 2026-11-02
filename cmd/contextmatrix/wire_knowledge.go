package main

import (
	"fmt"

	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/knowledge"
)

// wireKnowledge builds the knowledge service when providers are configured.
// Without providers it returns nil and never creates the audit log.
func wireKnowledge(cfg config.KnowledgeConfig) (*knowledge.Service, error) {
	if len(cfg.Providers) == 0 {
		return nil, nil
	}

	audit, err := knowledge.NewAuditLog(cfg.LogPath, knowledge.DefaultAuditMaxBytes, knowledge.DefaultAuditKeep)
	if err != nil {
		return nil, fmt.Errorf("knowledge audit log: %w", err)
	}

	return knowledge.New(cfg.Providers, audit, knowledge.Options{}), nil
}
