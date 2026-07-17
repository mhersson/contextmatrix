package sqliteutil

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDSN_PathFormats guards against a regression where url.URL.String()
// placed a relative path in the authority component (e.g. `file://images.db`),
// causing modernc.org/sqlite to error at first query with
// "invalid uri authority". Both absolute and relative paths must produce a
// DSN with no authority component, and must carry the synchronous=NORMAL
// pragma alongside journal_mode=WAL.
func TestDSN_PathFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{"absolute path", "/tmp/test.db", "file:/tmp/test.db?"},
		{"relative path", "test.db", "file:test.db?"},
		{"nested relative path", "data/test.db", "file:data/test.db?"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dsn := DSN(tc.path)

			// Reject the broken `file://...` (authority) form.
			assert.False(t, strings.HasPrefix(dsn, "file://"),
				"DSN must not place path in authority component: %q", dsn)
			assert.True(t, strings.HasPrefix(dsn, tc.want),
				"DSN must start with %q, got %q", tc.want, dsn)
			assert.Contains(t, dsn, "_pragma=journal_mode(WAL)")
			assert.Contains(t, dsn, "_pragma=synchronous(NORMAL)")
			assert.Contains(t, dsn, "_pragma=busy_timeout(5000)")
			assert.NotContains(t, dsn, "_pragma=foreign_keys(1)")
		})
	}
}

func TestDSN_WithForeignKeys(t *testing.T) {
	t.Parallel()

	dsn := DSN("test.db", WithForeignKeys())
	assert.Contains(t, dsn, "_pragma=foreign_keys(1)")
}

func TestOpen_CreatesParentDirs(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "dir", "test.db")

	db, err := Open(path)
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, db.Ping())

	_, err = os.Stat(filepath.Dir(path))
	require.NoError(t, err)
}

func TestMigrate_AppliesOnceAndIsIdempotent(t *testing.T) {
	t.Parallel()

	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	applied := 0
	migrations := []Migration{
		{
			Version: 1,
			Up: func(ctx context.Context, db *sql.DB) error {
				applied++

				_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS t (id INTEGER PRIMARY KEY)`)

				return err
			},
		},
	}

	ctx := context.Background()

	require.NoError(t, Migrate(ctx, db, "test", migrations))
	require.NoError(t, Migrate(ctx, db, "test", migrations))
	assert.Equal(t, 1, applied, "migration must run exactly once")

	var version int

	require.NoError(t, db.QueryRowContext(ctx, `SELECT version FROM schema_migrations`).Scan(&version))
	assert.Equal(t, 1, version)
}
