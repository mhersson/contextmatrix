package auth

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/golang-jwt/jwt/v5"
	githubauth "github.com/mhersson/contextmatrix-githubauth"
	"github.com/mhersson/contextmatrix/internal/authstore"
)

// Credential-pool errors. The HTTP layer maps shape and rejection to 422.
var (
	ErrInvalidCredential  = errors.New("auth: invalid credential")
	ErrCredentialRejected = errors.New("auth: credential rejected by GitHub")
	ErrNoCredentialKey    = errors.New("auth: credential key not configured")
)

// ErrCredentialUnavailable — missing, disabled, or undecryptable pool entry.
// Callers fail closed: never substitute the instance credential.
var ErrCredentialUnavailable = errors.New("auth: credential unavailable")

// CredentialInput is a create/rotate submission. Secret is plaintext in
// memory only — it is encrypted before storage and never returned.
type CredentialInput struct {
	Name           string
	Kind           authstore.CredentialKind
	Host           string
	APIBaseURL     string
	AppID          int64
	InstallationID int64
	Secret         string
}

// CredentialChecker validates a credential against GitHub before it is
// saved or rotated. Injectable so tests never call GitHub.
type CredentialChecker func(ctx context.Context, in CredentialInput) error

// credNameRe: 1-64 chars a-z 0-9 . _ -, no edge punctuation. The name is the
// immutable binding key .board.yaml references (S5).
var credNameRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?$`)

// SetCredentialKey wires the HKDF-derived 32-byte credential subkey.
func (s *Service) SetCredentialKey(key []byte) { s.credKey = key }

// SetCredentialChecker overrides the GitHub validation (tests).
func (s *Service) SetCredentialChecker(c CredentialChecker) { s.credCheck = c }

func (s *Service) checker() CredentialChecker {
	if s.credCheck != nil {
		return s.credCheck
	}

	return CheckCredentialAgainstGitHub
}

// CreateCredential validates (shape, then live against GitHub), encrypts,
// and stores a pool entry. Fail-early: a typo'd PAT dies here with GitHub's
// error, not days later inside an agent run.
func (s *Service) CreateCredential(ctx context.Context, in CredentialInput, createdBy string) error {
	if s.credKey == nil {
		return ErrNoCredentialKey
	}

	if err := validateCredentialShape(in); err != nil {
		return err
	}

	if err := s.checker()(ctx, in); err != nil {
		return fmt.Errorf("%w: %s", ErrCredentialRejected, err.Error())
	}

	blob, err := EncryptSecret(s.credKey, []byte(in.Secret))
	if err != nil {
		return err
	}

	return s.store.CreateCredential(ctx, &authstore.Credential{
		Name:            in.Name,
		Kind:            in.Kind,
		Host:            in.Host,
		APIBaseURL:      in.APIBaseURL,
		AppID:           in.AppID,
		InstallationID:  in.InstallationID,
		EncryptedSecret: blob,
		CreatedBy:       createdBy,
	}, s.now())
}

// RotateCredentialSecret replaces the secret under the same name, re-running
// the GitHub check with the stored metadata. Bindings never move.
func (s *Service) RotateCredentialSecret(ctx context.Context, name, secret string) error {
	if s.credKey == nil {
		return ErrNoCredentialKey
	}

	if secret == "" {
		return fmt.Errorf("%w: empty secret", ErrInvalidCredential)
	}

	stored, err := s.store.CredentialByName(ctx, name)
	if err != nil {
		return err
	}

	in := CredentialInput{
		Name: stored.Name, Kind: stored.Kind, Host: stored.Host,
		APIBaseURL: stored.APIBaseURL, AppID: stored.AppID,
		InstallationID: stored.InstallationID, Secret: secret,
	}

	if err := s.checker()(ctx, in); err != nil {
		return fmt.Errorf("%w: %s", ErrCredentialRejected, err.Error())
	}

	blob, err := EncryptSecret(s.credKey, []byte(secret))
	if err != nil {
		return err
	}

	return s.store.UpdateCredentialSecret(ctx, name, blob, s.now())
}

// UpdateCredentialMetadata edits the non-secret fields, re-validating the
// credential against GitHub with the DECRYPTED stored secret and the merged
// metadata — a host or installation change can silently invalidate an entry
// otherwise.
func (s *Service) UpdateCredentialMetadata(ctx context.Context, name, host, apiBaseURL string, appID, installationID int64) error {
	if s.credKey == nil {
		return ErrNoCredentialKey
	}

	stored, err := s.store.CredentialByName(ctx, name)
	if err != nil {
		return err
	}

	secret, err := DecryptSecret(s.credKey, stored.EncryptedSecret)
	if err != nil {
		return err
	}

	in := CredentialInput{
		Name: stored.Name, Kind: stored.Kind, Host: host,
		APIBaseURL: apiBaseURL, AppID: appID, InstallationID: installationID,
		Secret: string(secret),
	}

	if err := s.checker()(ctx, in); err != nil {
		return fmt.Errorf("%w: %s", ErrCredentialRejected, err.Error())
	}

	return s.store.UpdateCredentialMetadata(ctx, name, host, apiBaseURL, appID, installationID, s.now())
}

// SetCredentialDisabled toggles the softer alternative to deletion.
func (s *Service) SetCredentialDisabled(ctx context.Context, name string, disabled bool) error {
	return s.store.SetCredentialDisabled(ctx, name, disabled, s.now())
}

// DeleteCredential removes a pool entry.
// NOTE(S5): once .board.yaml bindings exist, this must refuse (409, listing
// the bound projects) while any project references the name.
func (s *Service) DeleteCredential(ctx context.Context, name string) error {
	return s.store.DeleteCredential(ctx, name)
}

// CredentialExists reports whether a pool entry with this name exists.
// Disabled entries still count as existing — .board.yaml bindings validate
// against the name space, not current usability; a disabled credential is a
// runtime resolution failure (fail-closed), not an invalid binding.
func (s *Service) CredentialExists(ctx context.Context, name string) (bool, error) {
	_, err := s.store.CredentialByName(ctx, name)
	if err != nil {
		if errors.Is(err, authstore.ErrNotFound) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

// ListCredentials returns pool entries with even the ciphertext stripped —
// no caller of this method has any business holding encrypted bytes.
func (s *Service) ListCredentials(ctx context.Context) ([]*authstore.Credential, error) {
	creds, err := s.store.ListCredentials(ctx)
	if err != nil {
		return nil, err
	}

	for _, c := range creds {
		c.EncryptedSecret = nil
	}

	return creds, nil
}

func validateCredentialShape(in CredentialInput) error {
	if !credNameRe.MatchString(in.Name) {
		return fmt.Errorf("%w: name must be 1-64 chars a-z 0-9 . _ - with no edge punctuation", ErrInvalidCredential)
	}

	if in.Secret == "" {
		return fmt.Errorf("%w: empty secret", ErrInvalidCredential)
	}

	switch in.Kind {
	case authstore.CredentialKindPAT:
		return nil
	case authstore.CredentialKindApp:
		if in.AppID == 0 || in.InstallationID == 0 {
			return fmt.Errorf("%w: app credentials require app_id and installation_id", ErrInvalidCredential)
		}

		if _, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(in.Secret)); err != nil {
			return fmt.Errorf("%w: private key does not parse: %s", ErrInvalidCredential, err.Error())
		}

		return nil
	default:
		return fmt.Errorf("%w: kind must be pat or app", ErrInvalidCredential)
	}
}

// credAPIBase resolves the GitHub API base: explicit override, else derived
// from the host, else public github.com. Mirrors config.ResolvedAPIBaseURL.
func credAPIBase(in CredentialInput) string {
	if in.APIBaseURL != "" {
		return in.APIBaseURL
	}

	if in.Host != "" {
		return "https://api." + in.Host
	}

	return "https://api.github.com"
}

// CheckCredentialAgainstGitHub is the default live validation: Apps mint an
// installation token; PATs probe /rate_limit. Both prove the credential
// actually works before it enters the pool.
func CheckCredentialAgainstGitHub(ctx context.Context, in CredentialInput) error {
	switch in.Kind {
	case authstore.CredentialKindApp:
		key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(in.Secret))
		if err != nil {
			return err
		}

		provider, err := githubauth.NewAppProviderWithKey(in.AppID, in.InstallationID, key, credAPIBase(in))
		if err != nil {
			return err
		}

		// TokenGenerator.GenerateToken returns (token, expiresAt, err); the
		// brief's sketch assumed a 2-value signature (adaptation point —
		// verified via `go doc ... TokenGenerator`).
		if _, _, err := provider.GenerateToken(ctx); err != nil {
			return err
		}

		return nil

	case authstore.CredentialKindPAT:
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, credAPIBase(in)+"/rate_limit", nil)
		if err != nil {
			return err
		}

		req.Header.Set("Authorization", "Bearer "+in.Secret)
		req.Header.Set("Accept", "application/vnd.github+json")

		client := &http.Client{Timeout: 15 * time.Second}

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("github returned %s for the token probe", resp.Status)
		}

		return nil

	default:
		return fmt.Errorf("unknown kind %q", in.Kind)
	}
}

// providerCacheEntry pins a built provider to the credential generation it
// was built from. authstore's UpdatedAt is whole-second granularity, and a
// single admin request can do a metadata update followed by a secret rotate
// as two sequential writes landing in the same second (see putCredential in
// internal/api/admin_credentials.go) — so UpdatedAt alone is NOT sufficient
// to make rotation self-invalidating. secretFP (a fingerprint of the
// encrypted secret) closes that gap: a cache hit requires both to match.
type providerCacheEntry struct {
	updatedAt time.Time
	secretFP  [32]byte
	provider  githubauth.TokenGenerator
	apiBase   string
	host      string
}

// TokenProviderFor resolves a pool entry into a ready, cached token provider.
func (s *Service) TokenProviderFor(ctx context.Context, name string) (githubauth.TokenGenerator, string, string, error) {
	if s.credKey == nil {
		return nil, "", "", ErrNoCredentialKey
	}

	stored, err := s.store.CredentialByName(ctx, name)
	if err != nil || stored.Disabled {
		return nil, "", "", fmt.Errorf("%w: %s", ErrCredentialUnavailable, name)
	}

	secretFP := sha256.Sum256(stored.EncryptedSecret)

	s.providerMu.Lock()
	if e, ok := s.providers[name]; ok && e.updatedAt.Equal(stored.UpdatedAt) && e.secretFP == secretFP {
		s.providerMu.Unlock()

		return e.provider, e.apiBase, e.host, nil
	}
	s.providerMu.Unlock()

	secret, err := DecryptSecret(s.credKey, stored.EncryptedSecret)
	if err != nil {
		return nil, "", "", fmt.Errorf("%w: %s: decrypt failed", ErrCredentialUnavailable, name)
	}

	apiBase := credAPIBase(CredentialInput{Host: stored.Host, APIBaseURL: stored.APIBaseURL})

	var inner githubauth.TokenGenerator

	switch stored.Kind {
	case authstore.CredentialKindPAT:
		inner, err = githubauth.NewPATProvider(string(secret))
	case authstore.CredentialKindApp:
		var key *rsa.PrivateKey

		key, err = jwt.ParseRSAPrivateKeyFromPEM(secret)
		if err == nil {
			inner, err = githubauth.NewAppProviderWithKey(stored.AppID, stored.InstallationID, key, apiBase)
		}
	default:
		err = fmt.Errorf("unknown kind %q", stored.Kind)
	}

	if err != nil {
		return nil, "", "", fmt.Errorf("%w: %s: %s", ErrCredentialUnavailable, name, err.Error())
	}

	provider := githubauth.NewCachingProvider(inner)

	s.providerMu.Lock()
	s.providers[name] = providerCacheEntry{
		updatedAt: stored.UpdatedAt, secretFP: secretFP, provider: provider, apiBase: apiBase, host: stored.Host,
	}
	s.providerMu.Unlock()

	return provider, apiBase, stored.Host, nil
}
