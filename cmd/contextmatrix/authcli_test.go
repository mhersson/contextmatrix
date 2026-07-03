package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeAuthConfig writes a minimal config.yaml the auth CLI can load and
// returns its path plus the (temp) auth.db and master.key paths it points at.
// mode is "multi" or "none".
func writeAuthConfig(t *testing.T, mode string) (cfgPath, dbPath, keyPath string) {
	t.Helper()

	dir := t.TempDir()
	dbPath = filepath.Join(dir, "auth.db")
	keyPath = filepath.Join(dir, "master.key")
	cfgPath = filepath.Join(dir, "config.yaml")

	cfg := "boards:\n" +
		"  dir: " + filepath.Join(dir, "boards") + "\n" +
		"github:\n" +
		"  auth_mode: pat\n" +
		"  pat:\n" +
		"    token: placeholder-not-a-real-token\n" +
		"auth:\n" +
		"  mode: " + mode + "\n" +
		"  db_path: " + dbPath + "\n" +
		"  master_key_file: " + keyPath + "\n"

	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))

	return cfgPath, dbPath, keyPath
}

// seedUser inserts an account directly into the auth store at dbPath.
func seedUser(t *testing.T, dbPath, username string, admin bool) {
	t.Helper()

	store, err := authstore.Open(dbPath)
	require.NoError(t, err)

	defer func() { _ = store.Close() }()

	_, err = store.CreateUser(context.Background(), username, "", admin, time.Now())
	require.NoError(t, err)
}

// seedDisabledAdmin inserts a disabled admin account directly into the auth
// store. It goes through the store's plain SetDisabled rather than
// auth.Service.SetUserDisabled because the service enforces a last-active-
// admin guard that would refuse to disable the sole admin in a fresh test
// store — irrelevant for seeding, since this account starts and stays the
// only user.
func seedDisabledAdmin(t *testing.T, dbPath, username string) {
	t.Helper()

	store, err := authstore.Open(dbPath)
	require.NoError(t, err)

	defer func() { _ = store.Close() }()

	ctx := context.Background()

	user, err := store.CreateUser(ctx, username, "", true, time.Now())
	require.NoError(t, err)

	require.NoError(t, store.SetDisabled(ctx, user.ID, true, time.Now()))
}

// extractToken pulls the raw token out of the first `/auth/token/<raw>` line.
func extractToken(t *testing.T, out string) string {
	t.Helper()

	const marker = "/auth/token/"

	idx := strings.Index(out, marker)
	require.GreaterOrEqual(t, idx, 0, "output missing token link:\n%s", out)

	rest := out[idx+len(marker):]
	raw := strings.FieldsFunc(rest, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ' ' || r == '\t' || r == ')'
	})

	require.NotEmpty(t, raw, "no token after marker:\n%s", out)

	return raw[0]
}

func TestAuthCLI_NoSubcommand(t *testing.T) {
	var out, errBuf bytes.Buffer

	code := authCLI(nil, &out, &errBuf)
	assert.NotZero(t, code)
	assert.Empty(t, out.String(), "usage goes to stderr, never stdout")
	assert.NotEmpty(t, errBuf.String())
}

func TestAuthCLI_UnknownSubcommand(t *testing.T) {
	var out, errBuf bytes.Buffer

	code := authCLI([]string{"frobnicate"}, &out, &errBuf)
	assert.NotZero(t, code)
	assert.Contains(t, errBuf.String(), "frobnicate")
}

func TestAuthCLI_ResetAdmin_UnknownUser(t *testing.T) {
	cfgPath, _, _ := writeAuthConfig(t, "multi")

	var out, errBuf bytes.Buffer

	code := authCLI([]string{"reset-admin", "--config", cfgPath, "ghost"}, &out, &errBuf)
	assert.Equal(t, 1, code)
	assert.Empty(t, out.String())
	assert.Contains(t, errBuf.String(), "ghost")
	assert.Contains(t, strings.ToLower(errBuf.String()), "no user")
}

func TestAuthCLI_ResetAdmin_NonAdmin(t *testing.T) {
	cfgPath, dbPath, _ := writeAuthConfig(t, "multi")
	seedUser(t, dbPath, "bob", false)

	var out, errBuf bytes.Buffer

	code := authCLI([]string{"reset-admin", "--config", cfgPath, "bob"}, &out, &errBuf)
	assert.Equal(t, 1, code)
	assert.Empty(t, out.String())
	assert.Contains(t, strings.ToLower(errBuf.String()), "not an admin")
}

func TestAuthCLI_ResetAdmin_DisabledAdmin(t *testing.T) {
	cfgPath, dbPath, _ := writeAuthConfig(t, "multi")
	seedDisabledAdmin(t, dbPath, "alice")

	var out, errBuf bytes.Buffer

	code := authCLI([]string{"reset-admin", "--config", cfgPath, "alice"}, &out, &errBuf)
	assert.Equal(t, 1, code)
	assert.Empty(t, out.String(), "no token line for a disabled admin")
	assert.Contains(t, strings.ToLower(errBuf.String()), "disabled")
}

func TestAuthCLI_ResetAdmin_IssuesResetToken(t *testing.T) {
	cfgPath, dbPath, _ := writeAuthConfig(t, "multi")
	seedUser(t, dbPath, "alice", true)

	var out, errBuf bytes.Buffer

	code := authCLI([]string{"reset-admin", "--config", cfgPath, "alice"}, &out, &errBuf)
	require.Equal(t, 0, code, "stderr: %s", errBuf.String())
	assert.Empty(t, errBuf.String())
	assert.Contains(t, out.String(), "/auth/token/")

	// Behaviour, not bytes: the printed link resolves to a live RESET token
	// targeting alice.
	raw := extractToken(t, out.String())

	store, err := authstore.Open(dbPath)
	require.NoError(t, err)

	defer func() { _ = store.Close() }()

	svc := auth.NewService(store, time.Hour)

	info, err := svc.InspectToken(context.Background(), raw)
	require.NoError(t, err)
	assert.Equal(t, authstore.TokenPurposeReset, info.Purpose)
	assert.Equal(t, "alice", info.Username)
}

func TestAuthCLI_RefusesNoneMode(t *testing.T) {
	cfgPath, dbPath, _ := writeAuthConfig(t, "none")
	seedUser(t, dbPath, "alice", true)

	for _, args := range [][]string{
		{"reset-admin", "--config", cfgPath, "alice"},
		{"rotate-master-key", "--config", cfgPath},
	} {
		var out, errBuf bytes.Buffer

		code := authCLI(args, &out, &errBuf)
		assert.Equal(t, 1, code, "args: %v", args)
		assert.Empty(t, out.String())
		assert.Contains(t, strings.ToLower(errBuf.String()), "multi", "args: %v", args)
	}
}

func TestAuthCLI_RotateMasterKey_RoundTrip(t *testing.T) {
	cfgPath, dbPath, keyPath := writeAuthConfig(t, "multi")

	// Seed: master key A on disk, one credential encrypted under derive(A).
	masterA := make([]byte, 32)
	_, err := rand.Read(masterA)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(keyPath, []byte(hex.EncodeToString(masterA)+"\n"), 0o600))

	keyA, err := auth.DeriveKey(masterA, auth.KeyPurposeCredentials)
	require.NoError(t, err)

	store, err := authstore.Open(dbPath)
	require.NoError(t, err)

	svc := auth.NewService(store, time.Hour)
	svc.SetCredentialKey(keyA)
	svc.SetCredentialChecker(func(context.Context, auth.CredentialInput) error { return nil })
	require.NoError(t, svc.CreateCredential(context.Background(),
		auth.CredentialInput{Name: "acme", Kind: authstore.CredentialKindPAT, Secret: "placeholder-secret"}, "human:root"))
	require.NoError(t, store.Close())

	// Rotate.
	var out, errBuf bytes.Buffer

	code := authCLI([]string{"rotate-master-key", "--config", cfgPath}, &out, &errBuf)
	require.Equal(t, 0, code, "stderr: %s", errBuf.String())
	assert.Contains(t, out.String(), "1 credential")

	// Reworded success output: states plainly that the .bak file is
	// reference-only and cannot roll the rotation back (assert on stable
	// substrings, never the literal key/token bytes).
	assert.Contains(t, out.String(), "New key installed at")
	assert.Contains(t, out.String(), "will NOT roll the rotation back")
	assert.Contains(t, out.String(), "safe to delete")

	// The staging file is fully consumed by a successful rotation.
	_, err = os.Stat(keyPath + ".new")
	assert.True(t, os.IsNotExist(err), "%s.new must not survive a successful rotation", keyPath)

	// Old key backed up verbatim; the live key file changed.
	bak, err := os.ReadFile(keyPath + ".bak")
	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(masterA)+"\n", string(bak), "backup holds the old key")

	newKeyHex, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	assert.NotEqual(t, string(bak), string(newKeyHex), "master key file rotated")

	masterB, err := hex.DecodeString(strings.TrimSpace(string(newKeyHex)))
	require.NoError(t, err)
	keyB, err := auth.DeriveKey(masterB, auth.KeyPurposeCredentials)
	require.NoError(t, err)

	// The pool now resolves under the NEW key and no longer under the old one.
	store2, err := authstore.Open(dbPath)
	require.NoError(t, err)

	defer func() { _ = store2.Close() }()

	svc2 := auth.NewService(store2, time.Hour)
	svc2.SetCredentialKey(keyB)

	_, _, _, err = svc2.TokenProviderFor(context.Background(), "acme")
	require.NoError(t, err, "credential resolves under the rotated key")

	stored, err := store2.CredentialByName(context.Background(), "acme")
	require.NoError(t, err)

	_, err = auth.DecryptSecret(keyB, stored.EncryptedSecret)
	require.NoError(t, err, "new key decrypts the rotated blob")

	_, err = auth.DecryptSecret(keyA, stored.EncryptedSecret)
	assert.ErrorIs(t, err, auth.ErrDecrypt, "old key no longer decrypts")
}

// TestAuthCLI_RotateMasterKey_StaleStaging simulates a previous rotation that
// crashed or aborted after staging <path>.new but before installing it: a
// garbage file is already sitting there when this run starts. The run must
// silently overwrite it with its own staged key rather than tripping over
// it, and the final installed key must be the one this run generated.
func TestAuthCLI_RotateMasterKey_StaleStaging(t *testing.T) {
	cfgPath, dbPath, keyPath := writeAuthConfig(t, "multi")

	masterA := make([]byte, 32)
	_, err := rand.Read(masterA)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(keyPath, []byte(hex.EncodeToString(masterA)+"\n"), 0o600))

	keyA, err := auth.DeriveKey(masterA, auth.KeyPurposeCredentials)
	require.NoError(t, err)

	store, err := authstore.Open(dbPath)
	require.NoError(t, err)

	svc := auth.NewService(store, time.Hour)
	svc.SetCredentialKey(keyA)
	svc.SetCredentialChecker(func(context.Context, auth.CredentialInput) error { return nil })
	require.NoError(t, svc.CreateCredential(context.Background(),
		auth.CredentialInput{Name: "acme", Kind: authstore.CredentialKindPAT, Secret: "placeholder-secret"}, "human:root"))
	require.NoError(t, store.Close())

	// A stale .new from a previously interrupted rotation — garbage, not
	// even valid hex.
	require.NoError(t, os.WriteFile(keyPath+".new", []byte("leftover-garbage-not-a-key\n"), 0o600))

	var out, errBuf bytes.Buffer

	code := authCLI([]string{"rotate-master-key", "--config", cfgPath}, &out, &errBuf)
	require.Equal(t, 0, code, "stderr: %s", errBuf.String())

	// The stale file did not survive — this run's own staged key replaced
	// and then consumed it.
	_, err = os.Stat(keyPath + ".new")
	assert.True(t, os.IsNotExist(err), "%s.new must not survive a successful rotation", keyPath)

	// Round-trip via the same helpers TestAuthCLI_RotateMasterKey_RoundTrip
	// uses: the installed key must decrypt the pool's current state.
	newKeyHex, err := os.ReadFile(keyPath)
	require.NoError(t, err)

	masterB, err := hex.DecodeString(strings.TrimSpace(string(newKeyHex)))
	require.NoError(t, err, "installed key file must parse as a server-format key, not the stale garbage")

	keyB, err := auth.DeriveKey(masterB, auth.KeyPurposeCredentials)
	require.NoError(t, err)

	store2, err := authstore.Open(dbPath)
	require.NoError(t, err)

	defer func() { _ = store2.Close() }()

	svc2 := auth.NewService(store2, time.Hour)
	svc2.SetCredentialKey(keyB)

	_, _, _, err = svc2.TokenProviderFor(context.Background(), "acme")
	require.NoError(t, err, "credential resolves under the rotated key despite the stale .new file")
}

// TestAuthCLI_RotateMasterKey_RefusesRetryThatWouldDestroyLiveKey reproduces
// the exact crash/retry scenario an unconditional-overwrite staging step is
// vulnerable to: a previous rotation committed the re-encrypted pool (so the
// pool now decrypts ONLY under X) but crashed before installing the new key
// file, leaving <path> = the OLD key W and <path>.new = the LIVE key X — the
// only surviving copy of the key the pool now actually needs. An operator's
// natural response is to just run the command again. The pre-stage probe
// must catch that <path> (W) does not decrypt the pool and refuse BEFORE
// touching <path>.new, or that retry would silently destroy X and strand the
// pool permanently.
func TestAuthCLI_RotateMasterKey_RefusesRetryThatWouldDestroyLiveKey(t *testing.T) {
	cfgPath, dbPath, keyPath := writeAuthConfig(t, "multi")

	// The pool is encrypted under X, as if a prior rotation already
	// committed the re-encrypt transaction under this key.
	masterX := make([]byte, 32)
	_, err := rand.Read(masterX)
	require.NoError(t, err)

	keyX, err := auth.DeriveKey(masterX, auth.KeyPurposeCredentials)
	require.NoError(t, err)

	store, err := authstore.Open(dbPath)
	require.NoError(t, err)

	svc := auth.NewService(store, time.Hour)
	svc.SetCredentialKey(keyX)
	svc.SetCredentialChecker(func(context.Context, auth.CredentialInput) error { return nil })
	require.NoError(t, svc.CreateCredential(context.Background(),
		auth.CredentialInput{Name: "acme", Kind: authstore.CredentialKindPAT, Secret: "placeholder-secret"}, "human:root"))
	require.NoError(t, store.Close())

	// <path> holds a DIFFERENT key W — the old key from before the crashed
	// rotation. LoadOrCreateMasterKey will read this as "oldMaster".
	masterW := make([]byte, 32)
	_, err = rand.Read(masterW)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(keyPath, []byte(hex.EncodeToString(masterW)+"\n"), 0o600))

	// <path>.new holds X — the crashed rotation's only surviving recovery
	// artifact.
	stagedPath := keyPath + ".new"
	stagedBefore := []byte(hex.EncodeToString(masterX) + "\n")
	require.NoError(t, os.WriteFile(stagedPath, stagedBefore, 0o600))

	var out, errBuf bytes.Buffer

	code := authCLI([]string{"rotate-master-key", "--config", cfgPath}, &out, &errBuf)
	assert.NotEqual(t, 0, code)
	assert.Empty(t, out.String())
	assert.Contains(t, errBuf.String(), stagedPath, "stderr must point the operator at the surviving .new file")
	assert.Contains(t, strings.ToLower(errBuf.String()), "previous rotation")

	// The crux of the fix: the recovery artifact must survive byte-for-byte.
	stagedAfter, err := os.ReadFile(stagedPath)
	require.NoError(t, err)
	assert.Equal(t, stagedBefore, stagedAfter, "%s must be untouched by a refused rotation", stagedPath)

	// No backup, no partial install — a refusal must not write anything.
	_, err = os.Stat(keyPath + ".bak")
	assert.True(t, os.IsNotExist(err), "a refused rotation must not write a backup")

	// The pool still decrypts under X — proof the DB was never touched.
	store2, err := authstore.Open(dbPath)
	require.NoError(t, err)

	defer func() { _ = store2.Close() }()

	svc2 := auth.NewService(store2, time.Hour)
	svc2.SetCredentialKey(keyX)

	_, _, _, err = svc2.TokenProviderFor(context.Background(), "acme")
	require.NoError(t, err, "pool must still decrypt under X after the refusal")
}

// TestAuthCLI_RotateMasterKey_RefusesWhenPoolUndecryptable covers the same
// pre-stage probe with no <path>.new in play at all: the key at <path>
// simply does not decrypt the pool (e.g. the wrong key file was restored).
// Rotation must refuse plainly, without staging anything.
func TestAuthCLI_RotateMasterKey_RefusesWhenPoolUndecryptable(t *testing.T) {
	cfgPath, dbPath, keyPath := writeAuthConfig(t, "multi")

	masterA := make([]byte, 32)
	_, err := rand.Read(masterA)
	require.NoError(t, err)

	keyA, err := auth.DeriveKey(masterA, auth.KeyPurposeCredentials)
	require.NoError(t, err)

	store, err := authstore.Open(dbPath)
	require.NoError(t, err)

	svc := auth.NewService(store, time.Hour)
	svc.SetCredentialKey(keyA)
	svc.SetCredentialChecker(func(context.Context, auth.CredentialInput) error { return nil })
	require.NoError(t, svc.CreateCredential(context.Background(),
		auth.CredentialInput{Name: "acme", Kind: authstore.CredentialKindPAT, Secret: "placeholder-secret"}, "human:root"))
	require.NoError(t, store.Close())

	// <path> holds an unrelated key that never encrypted anything in this
	// pool.
	masterW := make([]byte, 32)
	_, err = rand.Read(masterW)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(keyPath, []byte(hex.EncodeToString(masterW)+"\n"), 0o600))

	var out, errBuf bytes.Buffer

	code := authCLI([]string{"rotate-master-key", "--config", cfgPath}, &out, &errBuf)
	assert.NotEqual(t, 0, code)
	assert.Empty(t, out.String())
	assert.Contains(t, strings.ToLower(errBuf.String()), "does not decrypt")

	_, err = os.Stat(keyPath + ".new")
	assert.True(t, os.IsNotExist(err), "a refused rotation must not create a staging file")

	_, err = os.Stat(keyPath + ".bak")
	assert.True(t, os.IsNotExist(err), "a refused rotation must not write a backup")
}

func TestAuthCLI_RotateMasterKey_NoCredentials(t *testing.T) {
	cfgPath, _, keyPath := writeAuthConfig(t, "multi")

	masterA := make([]byte, 32)
	_, err := rand.Read(masterA)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(keyPath, []byte(hex.EncodeToString(masterA)+"\n"), 0o600))

	var out, errBuf bytes.Buffer

	code := authCLI([]string{"rotate-master-key", "--config", cfgPath}, &out, &errBuf)
	require.Equal(t, 0, code, "stderr: %s", errBuf.String())
	assert.Contains(t, out.String(), "0 credential")

	// The key still rotated even with an empty pool.
	newKeyHex, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	assert.NotEqual(t, hex.EncodeToString(masterA)+"\n", string(newKeyHex))
}
