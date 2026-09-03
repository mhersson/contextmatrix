package boardmerge

import "github.com/mhersson/contextmatrix/internal/board"

// applyInvariants re-checks the merged card against the project's rules. The
// repairs land in a later change; for now the field merge stands as-is.
func applyInvariants(card, _ *board.Card, _ string, _ Context) (*board.Card, []Resolution) {
	return card, nil
}
