package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// BenchmarkHeartbeat_Sequential measures the wall-clock latency of a single
// heartbeat through the full service write path (store write + git commit
// enqueue + commit wait).
func BenchmarkHeartbeat_Sequential(b *testing.B) {
	svc, _, _ := benchSetupService(b)

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title:    "HB",
		Type:     "task",
		Priority: "medium",
	})
	if err != nil {
		b.Fatalf("create: %v", err)
	}

	if _, err := svc.ClaimCard(ctx, "test-project", card.ID, "agent-1"); err != nil {
		b.Fatalf("claim: %v", err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := svc.HeartbeatCard(ctx, "test-project", card.ID, "agent-1"); err != nil {
			b.Fatalf("heartbeat: %v", err)
		}
	}
}

// BenchmarkHeartbeat_ConcurrentDistinctCards hammers 20 distinct cards in
// a single project. Because the queue uses one worker per project, commits
// still serialize — but writeMu is no longer held during the go-git call,
// so store writes in N goroutines can progress in parallel up to the
// enqueue boundary.
func BenchmarkHeartbeat_ConcurrentDistinctCards(b *testing.B) {
	svc, _, _ := benchSetupService(b)

	ctx := context.Background()

	const numCards = 20

	cardIDs := make([]string, numCards)

	for i := 0; i < numCards; i++ {
		card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
			Title:    fmt.Sprintf("HB %d", i),
			Type:     "task",
			Priority: "medium",
		})
		if err != nil {
			b.Fatalf("create %d: %v", i, err)
		}

		cardIDs[i] = card.ID

		if _, err := svc.ClaimCard(ctx, "test-project", card.ID, fmt.Sprintf("agent-%d", i)); err != nil {
			b.Fatalf("claim %d: %v", i, err)
		}
	}

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		var wg sync.WaitGroup

		for i := 0; i < numCards; i++ {
			wg.Add(1)

			go func(i int) {
				defer wg.Done()

				if err := svc.HeartbeatCard(ctx, "test-project", cardIDs[i], fmt.Sprintf("agent-%d", i)); err != nil {
					b.Errorf("heartbeat %d: %v", i, err)
				}
			}(i)
		}

		wg.Wait()
	}
}

// benchSetupService is a trimmed setupTest that skips t.Cleanup-style
// registration; the benchmark harness handles cleanup via tb.TempDir.
func benchSetupService(b *testing.B) (*CardService, string, func()) {
	return setupTestB(b)
}

// setupTestB builds a CardService for benchmarks (no t.Cleanup hooks).
// Mirrors setupTest(t) but takes *testing.B.
func setupTestB(b *testing.B) (*CardService, string, func()) {
	b.Helper()

	// Reuse the test helper's logic by shimming a *testing.T. Since b.TempDir()
	// and t.TempDir() both delegate to testing.TB, we call the helper via the
	// TB interface.
	return setupTestTB(b)
}

// BenchmarkHeartbeat_ConcurrentAcrossProjects measures the throughput gain
// that the per-project commit queue is designed to deliver: many projects
// running commits in parallel. Contrast with ConcurrentDistinctCards
// (single project) which remains limited by the per-project worker.
func BenchmarkHeartbeat_ConcurrentAcrossProjects(b *testing.B) {
	const numProjects = 10

	svc := newMultiProjectService(b, numProjects)

	ctx := context.Background()

	type claim struct {
		project, cardID, agent string
	}

	claims := make([]claim, numProjects)

	for i := 0; i < numProjects; i++ {
		proj := fmt.Sprintf("proj-%d", i)

		card, err := svc.CreateCard(ctx, proj, CreateCardInput{
			Title:    "HB",
			Type:     "task",
			Priority: "medium",
		})
		if err != nil {
			b.Fatalf("create %d: %v", i, err)
		}

		agent := fmt.Sprintf("agent-%d", i)

		if _, err := svc.ClaimCard(ctx, proj, card.ID, agent); err != nil {
			b.Fatalf("claim %d: %v", i, err)
		}

		claims[i] = claim{proj, card.ID, agent}
	}

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		var wg sync.WaitGroup

		for i := 0; i < numProjects; i++ {
			wg.Add(1)

			go func(c claim) {
				defer wg.Done()

				if err := svc.HeartbeatCard(ctx, c.project, c.cardID, c.agent); err != nil {
					b.Errorf("heartbeat %s: %v", c.cardID, err)
				}
			}(claims[i])
		}

		wg.Wait()
	}
}

// newMultiProjectService provisions a fresh CardService plus N seeded
// projects. The caller is responsible for orchestrating commits.
func newMultiProjectService(b *testing.B, numProjects int) *CardService {
	b.Helper()

	tmpDir := b.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(b, os.MkdirAll(boardsDir, 0o755))

	for i := 0; i < numProjects; i++ {
		proj := fmt.Sprintf("proj-%d", i)

		projectDir := filepath.Join(boardsDir, proj)
		require.NoError(b, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))

		cfg := testProject()
		cfg.Name = proj
		cfg.Prefix = fmt.Sprintf("P%d", i)

		require.NoError(b, board.SaveProjectConfig(projectDir, cfg))
	}

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(b, err)

	gitMgr, err := gitops.NewManager(boardsDir, "", "test", gitopsTestProvider(b))
	require.NoError(b, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)
	svc := NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)

	q := gitops.NewCommitQueue(gitMgr, 0)
	svc.SetCommitQueue(q)

	b.Cleanup(func() { _ = q.Close(context.Background()) })

	return svc
}
