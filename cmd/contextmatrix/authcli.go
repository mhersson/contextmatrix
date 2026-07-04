package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/mhersson/contextmatrix/internal/config"
)

// cliServiceIdleTTL is the session idle TTL passed to auth.NewService for the
// CLI. Neither subcommand touches sessions — token issuance and pool rotation
// ignore it — so the value is nominal.
const cliServiceIdleTTL = time.Hour

// masterKeyLen is the byte length of a freshly generated master key. Mirrors
// the format auth.LoadOrCreateMasterKey reads (hex-encoded 32 bytes).
const masterKeyLen = 32

const authUsage = `usage: contextmatrix auth <subcommand> [flags]

subcommands:
  reset-admin [--config PATH] <username>   issue a password-reset link for a locked-out admin
  rotate-master-key [--config PATH]        re-encrypt the credential pool under a fresh master key`

// runAuthCLI is the entry point for the `contextmatrix auth ...` subcommand,
// wired from main() before the server path. It returns a process exit code.
func runAuthCLI(args []string) int {
	return authCLI(args, os.Stdout, os.Stderr)
}

// authCLI dispatches an auth subcommand. Operator output (reset links,
// rotation summaries) goes to stdout; every error goes to stderr. Secrets are
// never sent through slog — the operator's terminal is the only channel.
func authCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, authUsage)

		return 2
	}

	switch args[0] {
	case "reset-admin":
		return runResetAdmin(args[1:], stdout, stderr)
	case "rotate-master-key":
		return runRotateMasterKey(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "auth: unknown subcommand %q\n\n%s\n", args[0], authUsage)

		return 2
	}
}

// loadAuthConfig registers the shared --config flag on fs, parses args, loads
// the config, and enforces auth.mode: multi. On any failure it writes to
// stderr and returns ok=false with the exit code to use.
func loadAuthConfig(fs *flag.FlagSet, args []string, stderr io.Writer) (cfg *config.Config, code int, ok bool) {
	configPath := fs.String("config", "", "path to config file (defaults to XDG discovery)")

	if err := fs.Parse(args); err != nil {
		return nil, 2, false // flag already reported the error to stderr
	}

	path := *configPath
	if path == "" {
		if path = config.FindConfigPath(); path == "" {
			fmt.Fprintf(stderr, "auth %s: no config file found; use --config to specify a path\n", fs.Name())

			return nil, 1, false
		}
	}

	loaded, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "auth %s: load config: %v\n", fs.Name(), err)

		return nil, 1, false
	}

	if loaded.Auth.Mode != config.AuthModeMulti {
		fmt.Fprintf(stderr, "auth %s: requires auth.mode: %q (current mode: %q)\n",
			fs.Name(), config.AuthModeMulti, loaded.Auth.Mode)

		return nil, 1, false
	}

	return loaded, 0, true
}

// runResetAdmin issues a one-time password-reset link for a locked-out admin.
// Host access is root trust here (see the multi-user spec): anyone who can run
// this binary against the config already controls the instance.
func runResetAdmin(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("reset-admin", flag.ContinueOnError)
	fs.SetOutput(stderr)

	cfg, code, ok := loadAuthConfig(fs, args, stderr)
	if !ok {
		return code
	}

	username := fs.Arg(0)
	if username == "" {
		fmt.Fprintln(stderr, "auth reset-admin: usage: contextmatrix auth reset-admin [--config PATH] <username>")

		return 2
	}

	// flag.FlagSet stops parsing at the first non-flag argument, so
	// `reset-admin <username> --config PATH` never parses --config as a
	// flag at all: it lands here as extra positional arguments (fs.Arg(1)
	// onward) and would otherwise be silently dropped, leaving --config's
	// intended value unapplied. Reject rather than guess which argument the
	// operator meant.
	if fs.NArg() > 1 {
		fmt.Fprintf(stderr,
			"auth reset-admin: unexpected argument %q after username %q — flags must come before the username "+
				"(usage: contextmatrix auth reset-admin [--config PATH] <username>)\n",
			fs.Arg(1), username)

		return 2
	}

	store, err := authstore.Open(cfg.Auth.DBPath)
	if err != nil {
		fmt.Fprintf(stderr, "auth reset-admin: open auth store: %v\n", err)

		return 1
	}

	defer func() { _ = store.Close() }()

	ctx := context.Background()
	svc := auth.NewService(store, cliServiceIdleTTL)

	user, err := svc.UserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, authstore.ErrNotFound) {
			fmt.Fprintf(stderr, "auth reset-admin: no user named %q exists\n", username)

			return 1
		}

		fmt.Fprintf(stderr, "auth reset-admin: look up user: %v\n", err)

		return 1
	}

	if !user.IsAdmin {
		fmt.Fprintf(stderr,
			"auth reset-admin: user %q is not an admin; this tool only unlocks existing admins — promote a user via the admin UI or API instead\n",
			username)

		return 1
	}

	if user.Disabled {
		fmt.Fprintf(stderr,
			"auth reset-admin: user %q is disabled; a reset link can never be redeemed by a disabled account — re-enable them via the admin UI or API first\n",
			username)

		return 1
	}

	raw, err := svc.IssueResetToken(ctx, user.ID)
	if err != nil {
		fmt.Fprintf(stderr, "auth reset-admin: issue reset token: %v\n", err)

		return 1
	}

	validHours := int(auth.OneTimeTokenTTL.Hours())

	fmt.Fprintf(stdout, "Password-reset link for admin %q:\n\n", username)
	fmt.Fprintf(stdout, "    /auth/token/%s\n\n", raw)
	fmt.Fprintf(stdout, "Prefix it with this server's URL (e.g. https://cm.example.com/auth/token/%s).\n", raw)
	fmt.Fprintf(stdout, "The link is valid for %d hours and can be used once.\n", validHours)

	return 0
}

// runRotateMasterKey re-encrypts the whole credential pool under a fresh
// master key, then swaps the key file.
//
// Before anything is written, probeCurrentKeyDecryptsPool checks that the
// key currently at <path> actually decrypts the pool. This guards a retry
// scenario: if an earlier run committed the re-encrypted pool but crashed
// before installing the new key file, <path> still holds the OLD key while
// <path>.new holds the only key that decrypts the pool's current state. An
// operator's natural response — run the command again — would otherwise have
// staging unconditionally overwrite that <path>.new with yet another fresh
// key, destroying the pool's only remaining decryptable copy, before
// RotateMasterKey's own decrypt-with-old guard (which runs INSIDE the
// transaction, well after staging) ever gets a chance to notice <path> is
// stale. The probe catches this before any file is touched.
//
// Once the probe passes, the new key is staged durably at <path>.new —
// written and fsynced (file + directory) — BEFORE the re-encrypt transaction
// commits. That ordering closes the crash window between "pool committed
// under the new key" and "new key file installed": at every point from here
// on, at least one on-disk file (<path> while the pool is still under the
// old key, or <path>.new once it isn't) holds a key that decrypts the pool's
// current state. See stageMasterKeyFile and installMasterKeyFile for the two
// halves of that guarantee.
func runRotateMasterKey(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("rotate-master-key", flag.ContinueOnError)
	fs.SetOutput(stderr)

	cfg, code, ok := loadAuthConfig(fs, args, stderr)
	if !ok {
		return code
	}

	// flag.FlagSet stops parsing at the first non-flag argument, so a
	// forgotten --config (e.g. `rotate-master-key ./prod.yaml`) leaves the
	// path sitting here as an ignored positional argument instead of being
	// applied — silently falling back to XDG config discovery. Reject
	// rather than rotate the wrong instance's key.
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr,
			"auth rotate-master-key: unexpected argument %q — rotate-master-key takes no positional arguments "+
				"(did you mean --config %s?)\n",
			fs.Arg(0), fs.Arg(0))

		return 2
	}

	store, err := authstore.Open(cfg.Auth.DBPath)
	if err != nil {
		fmt.Fprintf(stderr, "auth rotate-master-key: open auth store: %v\n", err)

		return 1
	}

	defer func() { _ = store.Close() }()

	oldMaster, created, err := auth.LoadOrCreateMasterKey(cfg.Auth.MasterKeyFile)
	if err != nil {
		fmt.Fprintf(stderr, "auth rotate-master-key: load master key: %v\n", err)

		return 1
	}

	if created {
		// No key existed, so nothing was ever encrypted under a prior key.
		// LoadOrCreateMasterKey just wrote a fresh one — there is nothing to
		// re-encrypt or roll forward.
		fmt.Fprintf(stdout, "No existing master key at %s; wrote a fresh one. Nothing to re-encrypt.\n",
			cfg.Auth.MasterKeyFile)

		return 0
	}

	ctx := context.Background()

	if err := probeCurrentKeyDecryptsPool(ctx, store, oldMaster); err != nil {
		staged := cfg.Auth.MasterKeyFile + ".new"

		if _, statErr := os.Stat(staged); statErr == nil {
			fmt.Fprintf(stderr,
				"auth rotate-master-key: refusing to proceed: the master key at %s does not decrypt the "+
					"credential pool.\n", cfg.Auth.MasterKeyFile)
			fmt.Fprintf(stderr,
				"auth rotate-master-key: %s exists — this looks like a previous rotation that committed but "+
					"was never installed, and %s likely holds the live master key. Move it into place and "+
					"retry: mv %s %s\n",
				staged, staged, staged, cfg.Auth.MasterKeyFile)
		} else {
			fmt.Fprintf(stderr,
				"auth rotate-master-key: the credential pool does not decrypt under the current master key "+
					"file at %s (%v); refusing to proceed.\n",
				cfg.Auth.MasterKeyFile, err)
		}

		return 1
	}

	newMaster := make([]byte, masterKeyLen)
	if _, err := rand.Read(newMaster); err != nil {
		fmt.Fprintf(stderr, "auth rotate-master-key: generate new key: %v\n", err)

		return 1
	}

	if err := stageMasterKeyFile(cfg.Auth.MasterKeyFile, newMaster); err != nil {
		// Nothing has been mutated yet — the pool is untouched and still
		// under the old key. Bail out before starting the transaction.
		fmt.Fprintf(stderr, "auth rotate-master-key: stage new key: %v\n", err)

		return 1
	}

	svc := auth.NewService(store, cliServiceIdleTTL)

	n, err := svc.RotateMasterKey(ctx, oldMaster, newMaster)
	if err != nil {
		// The transaction rolled back — every write it made is undone, so
		// the pool is exactly as it was on disk before this run started.
		// <path> was never touched by this run, and the probe above already
		// confirmed — before any mutation happened — that the key it holds
		// decrypts the pool, so that guarantee still stands. The staged key
		// at <path>.new is orphaned but harmless: the next attempt's own
		// probe will pass against the still-good <path>, and staging will
		// overwrite it exactly as it did here.
		fmt.Fprintf(stderr, "auth rotate-master-key: re-encrypt credential pool: %v\n", err)

		return 1
	}

	if err := installMasterKeyFile(cfg.Auth.MasterKeyFile); err != nil {
		staged := cfg.Auth.MasterKeyFile + ".new"

		if _, statErr := os.Stat(staged); statErr == nil {
			// <path>.new still exists — the rename never happened (or
			// failed), so the pool is already committed under newMaster but
			// no installed file reflects that yet. This can no longer be
			// avoided, but it is not catastrophic: the new key survives on
			// disk regardless, staged and fsynced at <path>.new before the
			// commit above.
			fmt.Fprintf(stderr,
				"auth rotate-master-key: CRITICAL: pool re-encrypted but installing the key file failed: %v\n", err)
			fmt.Fprintf(stderr,
				"auth rotate-master-key: the new master key is staged at %s; move it to %s to complete the rotation\n",
				staged, cfg.Auth.MasterKeyFile)
		} else {
			// The rename already succeeded — <path> holds the new key.
			// Only the trailing directory fsync failed, so the rename's
			// durability against an immediate crash is unconfirmed; unlike
			// the branch above, the rotation is NOT stranded.
			fmt.Fprintf(stderr,
				"auth rotate-master-key: pool re-encrypted and %s already holds the new key, but fsyncing its "+
					"directory failed: %v — run `sync` and verify the file manually\n",
				cfg.Auth.MasterKeyFile, err)
		}

		return 1
	}

	fmt.Fprintf(stdout, "Rotated master key: %d credential(s) re-encrypted.\n", n)
	fmt.Fprintf(stdout, "New key installed at %s.\n", cfg.Auth.MasterKeyFile)
	fmt.Fprintf(stdout,
		"Previous key saved to %s.bak (reference only — the pool is already re-encrypted under the new key, "+
			"so restoring it will NOT roll the rotation back; safe to delete).\n",
		cfg.Auth.MasterKeyFile)
	fmt.Fprintln(stdout, "Restart the server so it loads the new key.")

	return 0
}

// probeCurrentKeyDecryptsPool reports whether the master key currently on
// disk at <path> (already loaded into currentMaster by the caller) still
// decrypts the credential pool, without mutating anything — no file is
// touched, no transaction is opened. It derives the credentials subkey via
// HKDF and runs the same AES-GCM decrypt helper Service.RotateMasterKey uses
// internally, against exactly one stored entry: the pool is uniformly
// encrypted under one key or it is fundamentally broken, so one entry is
// enough to catch a wrong key on disk without redoing the whole pool's
// decrypt work that RotateMasterKey will do anyway moments later. An empty
// pool passes trivially — nothing is encrypted under any key, so nothing is
// at risk.
//
// This MUST run — and pass — before runRotateMasterKey writes anything,
// including staging <path>.new. Without it, retrying after a rotation that
// committed the re-encrypted pool but crashed before installing the new key
// file would stage a fresh key straight over <path>.new — the pool's only
// remaining decryptable copy — before RotateMasterKey's own decrypt-with-old
// guard (which runs INSIDE the transaction, well after staging) ever gets a
// chance to notice the key on disk is stale. By then the recovery artifact
// is already gone and the pool is unrecoverable.
func probeCurrentKeyDecryptsPool(ctx context.Context, store *authstore.Store, currentMaster []byte) error {
	creds, err := store.ListCredentials(ctx)
	if err != nil {
		return fmt.Errorf("list credentials: %w", err)
	}

	if len(creds) == 0 {
		return nil
	}

	key, err := auth.DeriveKey(currentMaster, auth.KeyPurposeCredentials)
	if err != nil {
		return err
	}

	if _, err := auth.DecryptSecret(key, creds[0].EncryptedSecret); err != nil {
		return fmt.Errorf("credential %q: %w", creds[0].Name, err)
	}

	return nil
}

// stageMasterKeyFile durably writes newMaster to <path>.new in the exact
// format auth.LoadOrCreateMasterKey reads (hex + newline, 0600), fsyncing
// both the file and its containing directory so the staged key survives a
// crash. This MUST run — and succeed — before the re-encryption transaction
// commits: once the pool is committed under newMaster, a durable on-disk
// copy of that key has to already exist, or a crash before the final install
// (installMasterKeyFile) would strand the pool re-encrypted under a key that
// lives only in process memory. A stale <path>.new left behind by a
// previously crashed or aborted rotation is overwritten unconditionally —
// callers only reach this after probeCurrentKeyDecryptsPool has confirmed the
// key already at <path> decrypts the pool, so whatever stale copy sits at
// <path>.new is provably not the pool's only remaining key at that point,
// and is safe to clobber.
func stageMasterKeyFile(path string, newMaster []byte) error {
	staged := path + ".new"

	f, err := os.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create staged key file: %w", err)
	}

	if _, err := f.Write([]byte(hex.EncodeToString(newMaster) + "\n")); err != nil {
		_ = f.Close()

		return fmt.Errorf("write staged key: %w", err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()

		return fmt.Errorf("fsync staged key: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close staged key: %w", err)
	}

	return fsyncDir(filepath.Dir(staged))
}

// installMasterKeyFile backs the current key file up to <path>.bak (old
// contents preserved verbatim), then installs the already-staged <path>.new
// over path via rename. Call only after the re-encryption transaction has
// committed: from that point on the pool decrypts solely under the key
// already staged — and fsynced — at <path>.new by stageMasterKeyFile, so a
// failure anywhere in this function still leaves that file in place as the
// pool's only durable key until the rename below succeeds. os.Rename is
// atomic, so <path>.new is consumed only on success; a crash mid-rename
// leaves either the pre- or post-rename state, never a partial one. The
// containing directory is fsynced after the rename too, so the directory
// entry change the rename made — not just the bytes it now points at — is
// itself durable against a crash immediately afterward.
func installMasterKeyFile(path string) error {
	existing, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read current key for backup: %w", err)
	}

	if err := os.WriteFile(path+".bak", existing, 0o600); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}

	if err := os.Rename(path+".new", path); err != nil {
		return fmt.Errorf("install staged key: %w", err)
	}

	return fsyncDir(filepath.Dir(path))
}

// fsyncDir opens dir and fsyncs it. A file-level fsync durably persists a
// file's bytes but not necessarily the directory-entry change (create,
// rename) that made the file visible under its current name — fsyncing the
// containing directory closes that gap.
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open dir for fsync: %w", err)
	}

	defer func() { _ = d.Close() }()

	if err := d.Sync(); err != nil {
		return fmt.Errorf("fsync dir: %w", err)
	}

	return nil
}
