package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/sqliteutil"
)

func TestLLMChannel_RoundTrip(t *testing.T) {
	st := openLadderStore(t)
	ctx := context.Background()

	mode, at, by, err := st.LLMChannel(ctx)
	require.NoError(t, err)
	assert.Empty(t, mode)
	assert.True(t, at.IsZero())
	assert.Empty(t, by)

	require.NoError(t, st.PutLLMChannel(ctx, "claude_code", "human:alice"))
	require.NoError(t, st.PutLLMChannel(ctx, "endpoint", "human:bob"))

	mode, at, by, err = st.LLMChannel(ctx)
	require.NoError(t, err)
	assert.Equal(t, "endpoint", mode)
	assert.False(t, at.IsZero())
	assert.Equal(t, "human:bob", by)
}

func TestSchema_NewTablesKeepVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.db")

	s, err := Open(path)
	require.NoError(t, err)

	var v int

	require.NoError(t, s.db.QueryRow("PRAGMA user_version").Scan(&v))
	assert.Equal(t, 6, v)
	assert.True(t, tableExists(t, s.db, "llm_channel"))
	require.NoError(t, s.Close())

	db, err := sql.Open("sqlite", sqliteutil.DSN(path, sqliteutil.WithForeignKeys()))
	require.NoError(t, err)
	_, err = db.Exec("DROP TABLE llm_channel")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	s2, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })

	assert.True(t, tableExists(t, s2.db, "llm_channel"))
	require.NoError(t, s2.db.QueryRow("PRAGMA user_version").Scan(&v))
	assert.Equal(t, 6, v)
}
