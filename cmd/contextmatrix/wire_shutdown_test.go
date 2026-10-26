package main

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/backend/sessionlog"
	"github.com/mhersson/contextmatrix/internal/gitops"
)

// slowCommitter counts completed commits after a fixed latency.
type slowCommitter struct {
	delay time.Duration
	done  atomic.Int32
}

func (c *slowCommitter) CommitFile(context.Context, string, string) error {
	time.Sleep(c.delay)
	c.done.Add(1)

	return nil
}

func (c *slowCommitter) CommitFiles(context.Context, []string, string) error { return nil }

func (c *slowCommitter) CommitFilesShell(context.Context, []string, string) error { return nil }

func (c *slowCommitter) CommitAll(context.Context, string) error { return nil }

func (c *slowCommitter) ReloadRepo(context.Context) error { return nil }

// A shutdown ctx that ran out during the HTTP drain must not cut the
// commit-queue drain short: buffered commits still land before return.
func TestRunShutdownSequence_DrainsCommitQueueOnExpiredCtx(t *testing.T) {
	committer := &slowCommitter{delay: 200 * time.Millisecond}
	q := gitops.NewCommitQueueWithCommitter(committer, 0)

	for range 2 {
		q.Enqueue(gitops.CommitJob{Project: "alpha", Kind: gitops.CommitKindFile, Path: "alpha/a.md", Message: "m"})
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_ = runShutdownSequence(ctx, shutdownComponents{
		HTTPServer:   &http.Server{ReadHeaderTimeout: time.Second},
		SessionLog:   sessionlog.NewManager(),
		CommitQueues: []*gitops.CommitQueue{q},
		HTTPCancel:   func() {},
		AppCancel:    func() {},
	})

	assert.EqualValues(t, 2, committer.done.Load())
	require.Error(t, ctx.Err())
}
