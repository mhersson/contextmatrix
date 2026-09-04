package images

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWriter stands in for the card service: shared maps a project to the
// repo that stores its images, and WriteRepoImages writes the files where
// the index expects them, under dirs[repo].
type fakeWriter struct {
	shared map[string]string // project -> repo
	dirs   map[string]string // repo -> dir
	fail   error
	delay  time.Duration

	mu    sync.Mutex
	calls []writeCall
}

type writeCall struct {
	project string
	names   []string
}

func (w *fakeWriter) ImagesInRepo(_ context.Context, project string) (string, bool) {
	repo, ok := w.shared[project]

	return repo, ok
}

func (w *fakeWriter) WriteRepoImages(_ context.Context, project string, files []RepoImage) error {
	if w.delay > 0 {
		time.Sleep(w.delay)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.fail != nil {
		return w.fail
	}

	names := make([]string, 0, len(files))

	for _, f := range files {
		dir := filepath.Join(w.dirs[w.shared[project]], project, ImagesDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}

		if err := os.WriteFile(filepath.Join(dir, f.Name), f.Data, 0o644); err != nil {
			return err
		}

		names = append(names, f.Name)
	}

	w.calls = append(w.calls, writeCall{project: project, names: names})

	return nil
}

func (w *fakeWriter) writes() []writeCall {
	w.mu.Lock()
	defer w.mu.Unlock()

	return append([]writeCall(nil), w.calls...)
}

// countingStore wraps a Store and counts Get calls, so a test can prove a
// read was served without touching the database.
type countingStore struct {
	Store

	mu   sync.Mutex
	gets int
}

func (c *countingStore) Get(ctx context.Context, id string) ([]byte, string, error) {
	c.mu.Lock()
	c.gets++
	c.mu.Unlock()

	return c.Store.Get(ctx, id)
}

func (c *countingStore) getCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.gets
}

type layeredEnv struct {
	layered *Layered
	db      *countingStore
	writer  *fakeWriter
	team    *RepoIndex
	teamDir string
}

// newLayeredEnv wires one shared repo "team" holding project alpha (and
// gamma) next to a private project beta that has no repo index.
func newLayeredEnv(t *testing.T) *layeredEnv {
	t.Helper()

	teamDir := t.TempDir()
	team := NewRepoIndex("team", teamDir)
	db := &countingStore{Store: openTestStore(t)}
	writer := &fakeWriter{
		shared: map[string]string{"alpha": "team", "gamma": "team"},
		dirs:   map[string]string{"team": teamDir},
	}

	return &layeredEnv{
		layered: NewLayered(db, writer, team),
		db:      db, writer: writer, team: team, teamDir: teamDir,
	}
}

func processedID(t *testing.T, raw []byte) string {
	t.Helper()

	processed, _, err := Process(raw)
	require.NoError(t, err)

	return IDOf(processed)
}

func TestLayered_PutGoesToTheDatabase(t *testing.T) {
	t.Parallel()

	env := newLayeredEnv(t)
	ctx := context.Background()
	raw := makeTinyPNG(t)

	id, ct, err := env.layered.Put(ctx, raw)
	require.NoError(t, err)
	assert.Equal(t, "image/png", ct)
	assert.Empty(t, env.writer.writes())

	_, _, err = env.db.Store.Get(ctx, id)
	require.NoError(t, err, "the blob is in images.db")
	assert.False(t, env.team.Has("alpha", id))
}

func TestLayered_PutInRoutesByProject(t *testing.T) {
	t.Parallel()

	raw := makeTinyPNG(t)

	tests := []struct {
		name    string
		project string
		inRepo  bool
	}{
		{"empty project stays in the database", "", false},
		{"unknown project stays in the database", "nope", false},
		{"private project stays in the database", "beta", false},
		{"shared project goes to the repo", "alpha", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := newLayeredEnv(t)
			ctx := context.Background()

			id, ct, err := env.layered.PutIn(ctx, tt.project, raw)
			require.NoError(t, err)
			assert.Equal(t, processedID(t, raw), id)
			assert.Equal(t, "image/png", ct)

			_, _, dbErr := env.db.Store.Get(ctx, id)

			if !tt.inRepo {
				require.NoError(t, dbErr)
				assert.Empty(t, env.writer.writes())

				return
			}

			require.ErrorIs(t, dbErr, ErrNotFound, "a repo image never lands in images.db")
			require.Equal(t, []writeCall{{project: "alpha", names: []string{id + ".png"}}}, env.writer.writes())
			assert.True(t, env.team.Has("alpha", id))
			assert.FileExists(t, filepath.Join(env.teamDir, "alpha", ImagesDir, id+".png"))
		})
	}
}

func TestLayered_PutInWritesProcessedBytes(t *testing.T) {
	t.Parallel()

	env := newLayeredEnv(t)
	ctx := context.Background()
	raw := makeTinyPNG(t)

	id, _, err := env.layered.PutIn(ctx, "alpha", raw)
	require.NoError(t, err)

	processed, _, err := Process(raw)
	require.NoError(t, err)

	onDisk, err := os.ReadFile(filepath.Join(env.teamDir, "alpha", ImagesDir, id+".png"))
	require.NoError(t, err)
	assert.Equal(t, processed, onDisk, "the repo holds the re-encoded bytes, the same ones images.db would")
}

func TestLayered_PutInDedupesPerProject(t *testing.T) {
	t.Parallel()

	env := newLayeredEnv(t)
	ctx := context.Background()
	raw := makeTinyPNG(t)

	id1, _, err := env.layered.PutIn(ctx, "alpha", raw)
	require.NoError(t, err)

	id2, _, err := env.layered.PutIn(ctx, "alpha", raw)
	require.NoError(t, err)
	assert.Equal(t, id1, id2)
	assert.Len(t, env.writer.writes(), 1, "the same image in the same project is written once")

	id3, _, err := env.layered.PutIn(ctx, "gamma", raw)
	require.NoError(t, err)
	assert.Equal(t, id1, id3)
	assert.Len(t, env.writer.writes(), 2, "another project gets its own copy")
	assert.True(t, env.team.Has("gamma", id1))
}

func TestLayered_PutInWriterFailureLeavesNoIndexEntry(t *testing.T) {
	t.Parallel()

	env := newLayeredEnv(t)
	ctx := context.Background()
	sentinel := errors.New("commit failed")
	env.writer.fail = sentinel

	_, _, err := env.layered.PutIn(ctx, "alpha", makeTinyPNG(t))
	require.ErrorIs(t, err, sentinel)
	assert.Equal(t, 0, env.team.Len())
}

func TestLayered_PutInRejectsBadPayloadsLikeTheDatabase(t *testing.T) {
	t.Parallel()

	env := newLayeredEnv(t)
	ctx := context.Background()

	_, _, err := env.layered.PutIn(ctx, "alpha", []byte("not an image"))
	require.ErrorIs(t, err, ErrUnsupportedFormat)
	assert.Empty(t, env.writer.writes())
}

func TestLayered_GetPrefersTheRepoAndFallsThroughToTheDatabase(t *testing.T) {
	t.Parallel()

	env := newLayeredEnv(t)
	ctx := context.Background()

	repoID, _, err := env.layered.PutIn(ctx, "alpha", makeTinyPNG(t))
	require.NoError(t, err)

	before := env.db.getCount()

	data, ct, err := env.layered.Get(ctx, repoID)
	require.NoError(t, err)
	assert.Equal(t, "image/png", ct)
	assert.NotEmpty(t, data)
	assert.Equal(t, before, env.db.getCount(), "served from the repo index, the database was not asked")

	dbRaw := makeTinyPNGColor(t, 200)

	dbID, _, err := env.layered.Put(ctx, dbRaw)
	require.NoError(t, err)

	data, _, err = env.layered.Get(ctx, dbID)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
	assert.Equal(t, before+1, env.db.getCount(), "an id the repos do not hold falls through to images.db")

	_, _, err = env.layered.Get(ctx, "0000000000000000")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestLayered_WithoutReposDelegatesEverything(t *testing.T) {
	t.Parallel()

	db := openTestStore(t)
	layered := NewLayered(db, nil)
	ctx := context.Background()

	id, ct, err := layered.PutIn(ctx, "alpha", makeTinyPNG(t))
	require.NoError(t, err)
	assert.Equal(t, "image/png", ct)

	data, gotCT, err := layered.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, ct, gotCT)
	assert.NotEmpty(t, data)
}

func TestLayered_ExportWritesReferencedDatabaseImagesOnce(t *testing.T) {
	t.Parallel()

	env := newLayeredEnv(t)
	ctx := context.Background()

	idA, _, err := env.layered.Put(ctx, makeTinyPNGColor(t, 10))
	require.NoError(t, err)

	idB, _, err := env.layered.Put(ctx, makeTinyPNGColor(t, 20))
	require.NoError(t, err)

	idC, _, err := env.layered.Put(ctx, makeTinyPNGColor(t, 30))
	require.NoError(t, err)

	// B is already in the repo under alpha.
	dataB, _, err := env.db.Store.Get(ctx, idB)
	require.NoError(t, err)
	writeRepoImage(t, env.teamDir, "alpha", idB+".png", dataB)
	env.team.Add("alpha", idB, "image/png")

	refs := map[string][]string{
		"alpha": {idA, idB, idA, "1111111111111111"},
		"gamma": {idA},
		"empty": {},
	}

	n, err := env.layered.Export(ctx, "team", refs)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	assert.Equal(t, []writeCall{
		{project: "alpha", names: []string{idA + ".png"}},
		{project: "gamma", names: []string{idA + ".png"}},
	}, env.writer.writes(), "projects in name order, one write per project, B skipped, unknown id skipped")

	assert.True(t, env.team.Has("alpha", idA))
	assert.True(t, env.team.Has("gamma", idA))
	assert.False(t, env.team.Has("alpha", idC), "an unreferenced image stays in the database")

	n, err = env.layered.Export(ctx, "team", refs)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "a second export is a no-op")
	assert.Len(t, env.writer.writes(), 2)
}

func TestLayered_ExportUnknownRepo(t *testing.T) {
	t.Parallel()

	env := newLayeredEnv(t)

	_, err := env.layered.Export(context.Background(), "nope", nil)
	require.Error(t, err)
}

func TestLayered_ConcurrentIdenticalUploadsWriteOnce(t *testing.T) {
	t.Parallel()

	env := newLayeredEnv(t)
	env.writer.delay = 50 * time.Millisecond
	ctx := context.Background()
	raw := makeTinyPNG(t)

	var wg sync.WaitGroup

	ids := make([]string, 5)

	for i := range ids {
		wg.Go(func() {
			id, _, err := env.layered.PutIn(ctx, "alpha", raw)
			assert.NoError(t, err)

			ids[i] = id
		})
	}

	wg.Wait()

	for _, id := range ids[1:] {
		assert.Equal(t, ids[0], id)
	}

	assert.Len(t, env.writer.writes(), 1, "the same image uploaded concurrently into one project is written once")
	assert.True(t, env.team.Has("alpha", ids[0]))
}

func TestLayered_ExportStopsOnWriterFailure(t *testing.T) {
	t.Parallel()

	env := newLayeredEnv(t)
	ctx := context.Background()

	idA, _, err := env.layered.Put(ctx, makeTinyPNGColor(t, 10))
	require.NoError(t, err)

	sentinel := errors.New("commit failed")
	env.writer.fail = sentinel

	n, err := env.layered.Export(ctx, "team", map[string][]string{"alpha": {idA}})
	require.ErrorIs(t, err, sentinel)
	assert.Equal(t, 0, n)
	assert.False(t, env.team.Has("alpha", idA))
}
