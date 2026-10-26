package service

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// newTestCardService builds a one-repo CardService through
// NewCardServiceRepos, the constructor production wiring uses. An empty
// repo.Name defaults to DefaultRepoName.
func newTestCardService(tb testing.TB, store storage.Store, bus *events.Bus, tokenCosts map[string]ModelRate, repo *BoardsRepo) *CardService {
	tb.Helper()

	if repo.Name == "" {
		repo.Name = DefaultRepoName
	}

	svc, err := NewCardServiceRepos(store, bus, tokenCosts, repo)
	require.NoError(tb, err)

	return svc
}

// newTestPlaybookService builds a one-repo PlaybookService through
// NewPlaybookServiceRepos. An empty repo.Name defaults to DefaultRepoName.
func newTestPlaybookService(tb testing.TB, store PlaybookStore, cards storage.Store, bus *events.Bus, clk clock.Clock, repo *PlaybookRepo) *PlaybookService {
	tb.Helper()

	if repo.Name == "" {
		repo.Name = DefaultRepoName
	}

	svc, err := NewPlaybookServiceRepos(store, cards, bus, clk, repo)
	require.NoError(tb, err)

	return svc
}
