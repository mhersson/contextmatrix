package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPlaybook(id string) *board.Playbook {
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)

	return &board.Playbook{
		ID: id, Title: "Test " + id, Created: now, Updated: now, NextEntryID: 2,
		Entries: []board.PlaybookEntry{
			{ID: "e1", Type: board.EntryTypeManual, Text: "step one"},
		},
	}
}

func TestPlaybookStore_CreateGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFilesystemPlaybookStore(dir)
	require.NoError(t, err)

	p := testPlaybook("alpha")
	require.NoError(t, store.Create(context.Background(), p))

	got, err := store.Get(context.Background(), "alpha")
	require.NoError(t, err)
	assert.Equal(t, p, got)

	// File landed at playbooks/alpha.yaml.
	_, err = os.Stat(filepath.Join(dir, "playbooks", "alpha.yaml"))
	assert.NoError(t, err)
}

func TestPlaybookStore_CreateDuplicate(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFilesystemPlaybookStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.Create(context.Background(), testPlaybook("alpha")))

	err = store.Create(context.Background(), testPlaybook("alpha"))
	assert.ErrorIs(t, err, ErrPlaybookExists)
}

func TestPlaybookStore_CreateRefusesOrphanFile(t *testing.T) {
	// A file on disk that is NOT in the index (e.g. skipped as malformed)
	// must still block Create for the same id.
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "playbooks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "playbooks", "broken.yaml"), []byte("id: [unclosed"), 0o644))

	store, err := NewFilesystemPlaybookStore(dir) // loads, skips broken.yaml with a warning
	require.NoError(t, err)

	err = store.Create(context.Background(), testPlaybook("broken"))
	assert.ErrorIs(t, err, ErrPlaybookExists)
}

func TestPlaybookStore_SaveNotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFilesystemPlaybookStore(dir)
	require.NoError(t, err)

	err = store.Save(context.Background(), testPlaybook("ghost"))
	assert.ErrorIs(t, err, ErrPlaybookNotFound)
}

func TestPlaybookStore_GetNotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFilesystemPlaybookStore(dir)
	require.NoError(t, err)

	_, err = store.Get(context.Background(), "ghost")
	assert.ErrorIs(t, err, ErrPlaybookNotFound)
}

func TestPlaybookStore_DeleteRemovesFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFilesystemPlaybookStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.Create(context.Background(), testPlaybook("alpha")))

	require.NoError(t, store.Delete(context.Background(), "alpha"))

	_, err = store.Get(context.Background(), "alpha")
	require.ErrorIs(t, err, ErrPlaybookNotFound)

	_, err = os.Stat(filepath.Join(dir, "playbooks", "alpha.yaml"))
	assert.True(t, os.IsNotExist(err))
}

func TestPlaybookStore_ListSortedAndCopied(t *testing.T) {
	// Create "b" then "a"; List returns [a b]; mutating the returned
	// playbook's Entries does not affect a subsequent Get (deep copy).
	dir := t.TempDir()
	store, err := NewFilesystemPlaybookStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.Create(context.Background(), testPlaybook("b")))
	require.NoError(t, store.Create(context.Background(), testPlaybook("a")))

	list, err := store.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "a", list[0].ID)
	assert.Equal(t, "b", list[1].ID)

	// Mutate the returned playbook's entries; a fresh Get must be unaffected.
	list[0].Entries[0].Text = "mutated"

	got, err := store.Get(context.Background(), "a")
	require.NoError(t, err)
	assert.Equal(t, "step one", got.Entries[0].Text)
}

func TestPlaybookStore_MissingDirIsEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFilesystemPlaybookStore(dir)
	require.NoError(t, err)

	list, err := store.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestPlaybookStore_LoadSkipsDotfilesAndMalformed(t *testing.T) {
	// Seed playbooks/ with: good.yaml (valid), .hidden.yaml, notes.txt,
	// bad.yaml (unparseable). New store lists exactly [good].
	dir := t.TempDir()
	playbooksDir := filepath.Join(dir, "playbooks")
	require.NoError(t, os.MkdirAll(playbooksDir, 0o755))

	good := testPlaybook("good")
	data, err := board.SerializePlaybook(good)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(playbooksDir, "good.yaml"), data, 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(playbooksDir, ".hidden.yaml"), data, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(playbooksDir, "notes.txt"), []byte("not a playbook"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(playbooksDir, "bad.yaml"), []byte("id: [unclosed"), 0o644))

	store, err := NewFilesystemPlaybookStore(dir)
	require.NoError(t, err)

	list, err := store.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "good", list[0].ID)
}

func TestPlaybookStore_ReloadPicksUpExternalWrites(t *testing.T) {
	// Create store, write a new valid file directly to disk, ReloadIndex,
	// Get finds it. (Mirrors TestFilesystemStore_..._ReloadPicksUpExternalWrites.)
	dir := t.TempDir()
	store, err := NewFilesystemPlaybookStore(dir)
	require.NoError(t, err)

	ctx := context.Background()

	// Before reload, the store does not know about it.
	_, err = store.Get(ctx, "external")
	require.ErrorIs(t, err, ErrPlaybookNotFound)

	external := testPlaybook("external")
	data, err := board.SerializePlaybook(external)
	require.NoError(t, err)

	playbooksDir := filepath.Join(dir, "playbooks")
	require.NoError(t, os.MkdirAll(playbooksDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(playbooksDir, "external.yaml"), data, 0o644))

	require.NoError(t, store.ReloadIndex(ctx))

	got, err := store.Get(ctx, "external")
	require.NoError(t, err)
	assert.Equal(t, "external", got.ID)
}

func TestPlaybookStore_GuardsProjectCollision(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "playbooks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "playbooks", ".board.yaml"), []byte("name: playbooks\n"), 0o644))

	_, err := NewFilesystemPlaybookStore(dir)
	assert.ErrorIs(t, err, ErrPlaybooksDirIsProject)
}
