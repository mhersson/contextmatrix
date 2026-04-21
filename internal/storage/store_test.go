package storage

import (
	"fmt"
	"testing"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestErrProjectNotFound_AliasesBoardSentinel pins the invariant that the
// storage package's ErrProjectNotFound is the same underlying sentinel as
// board.ErrProjectNotFound. If the two ever diverge, errors originating from
// board.LoadProjectConfig silently miss the 404 branch in api.handleServiceError
// (which keys off storage.ErrProjectNotFound) and fall through to 500.
func TestErrProjectNotFound_AliasesBoardSentinel(t *testing.T) {
	t.Run("sentinels are identical values", func(t *testing.T) {
		assert.Same(t, board.ErrProjectNotFound, ErrProjectNotFound)
	})

	t.Run("errors.Is matches in both directions", func(t *testing.T) {
		require.ErrorIs(t, board.ErrProjectNotFound, ErrProjectNotFound)
		require.ErrorIs(t, ErrProjectNotFound, board.ErrProjectNotFound)
	})

	t.Run("wrapped board sentinel is still matched by storage sentinel", func(t *testing.T) {
		wrapped := fmt.Errorf("load project config: %w", board.ErrProjectNotFound)
		require.ErrorIs(t, wrapped, ErrProjectNotFound)
	})
}
