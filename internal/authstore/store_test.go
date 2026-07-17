package authstore_test

import (
	"path/filepath"
	"testing"

	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openTestStore opens a store on a fresh temp path. Helper reused by every
// test file in this package.
func openTestStore(t *testing.T) *authstore.Store {
	t.Helper()

	store, err := authstore.Open(filepath.Join(t.TempDir(), "sub", "auth.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	return store
}

func TestOpen_CreatesParentDirsAndMigrates(t *testing.T) {
	store := openTestStore(t)
	assert.NotNil(t, store)
}

func TestOpen_IsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.db")

	first, err := authstore.Open(path)
	require.NoError(t, err)
	require.NoError(t, first.Close())

	// Re-opening an already-migrated database must be a clean no-op - this is
	// the ledger property that makes upgrades safe.
	second, err := authstore.Open(path)
	require.NoError(t, err)
	require.NoError(t, second.Close())
}
