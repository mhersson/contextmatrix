package service

import "context"

// SyncMutation is a write the syncer runs inside one shared cycle: Apply
// after the merge, under both write locks; Undo when the cycle fails after
// Apply ran, so a write the caller is told failed never reaches the remote
// on a later push. Undo runs under the same locks and must leave the tree
// clean. Both may be nil.
type SyncMutation struct {
	Apply func(ctx context.Context) error
	Undo  func(ctx context.Context) error
}
