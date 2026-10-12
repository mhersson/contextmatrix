package jirawriteback

import "context"

func (w *Writeback) runWorker(ctx context.Context) { <-ctx.Done() }
