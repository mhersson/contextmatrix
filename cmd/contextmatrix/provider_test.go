package main

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"
	"time"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/authstore"
	"github.com/mhersson/contextmatrix/internal/board"
)

// fakeProjectGetter is a canned projectGetter test double. Project lookup is
// not the logic under test here, so it always returns the same result
// regardless of the requested name.
type fakeProjectGetter struct {
	cfg *board.ProjectConfig
	err error
}

func (f fakeProjectGetter) GetProject(_ context.Context, _ string) (*board.ProjectConfig, error) {
	return f.cfg, f.err
}

// fakeInstanceTokenProvider is a githubauth.TokenGenerator test double
// standing in for the instance-wide fallback provider - distinct from
// whatever newTestAuthService's credential pool produces, so assert.Same /
// assert.NotSame prove which provider newProviderForProject actually chose.
type fakeInstanceTokenProvider struct {
	token string
}

func (f *fakeInstanceTokenProvider) GenerateToken(_ context.Context) (string, time.Time, error) {
	return f.token, time.Time{}, nil
}

// newTestAuthService builds a real *auth.Service over a real authstore in
// t.TempDir with one genuinely resolvable credential ("good-cred")
// registered. Mirrors the seeding approach in
// cmd/contextmatrix/authcli_test.go and internal/api/backend_test.go's
// TestRunCard_ProviderForProject_BrokenBindingNeverFallsBackToInstance: real
// crypto, real store, so a binding to a name that was never registered here
// ("missing-cred") fails through the actual TokenProviderFor resolution
// path, not a stand-in.
func newTestAuthService(t *testing.T) *auth.Service {
	t.Helper()

	authStore, err := authstore.Open(filepath.Join(t.TempDir(), "auth.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = authStore.Close() })

	authSvc := auth.NewService(authStore, time.Hour)

	credKey := make([]byte, 32)
	_, err = rand.Read(credKey)
	require.NoError(t, err)

	authSvc.SetCredentialKey(credKey)
	authSvc.SetCredentialChecker(func(context.Context, auth.CredentialInput) error { return nil })

	require.NoError(t, authSvc.CreateCredential(context.Background(), auth.CredentialInput{
		Name: "good-cred", Kind: authstore.CredentialKindPAT, Secret: "good-secret",
	}, "human:root"))

	return authSvc
}

// TestNewProviderForProject directly exercises newProviderForProject - the
// extracted, named form of the resolution logic that used to live only as an
// inline closure in main() (see provider.go). Before this extraction,
// internal/api/backend_test.go could only pin a hand-typed replica of this
// logic; a regression in the real closure (e.g. the fail-closed
// `return nil, "", err` silently changed to fall back to the instance
// provider) would have been caught by nothing.
func TestNewProviderForProject(t *testing.T) {
	authSvc := newTestAuthService(t)
	instanceProvider := &fakeInstanceTokenProvider{token: "instance-token"}

	const instanceAPIBase = "https://instance.example/api"

	t.Run("broken binding never falls back to the instance provider", func(t *testing.T) {
		projects := fakeProjectGetter{cfg: &board.ProjectConfig{Name: "proj", GitHubCredential: "missing-cred"}}
		resolve := newProviderForProject(projects, authSvc, instanceProvider, instanceAPIBase)

		provider, apiBase, err := resolve(context.Background(), "proj")

		require.ErrorIs(t, err, auth.ErrCredentialUnavailable)
		// assert.Nil is the correct (and only testify-legal) way to prove
		// "never the instance provider" here: provider is a bare nil
		// interface, and testify's assert.NotSame refuses to compare a
		// non-pointer against a pointer at all (fails with "Both arguments
		// must be pointers" instead of evaluating true/false). Nil already
		// strictly implies "not equal to any non-nil instanceProvider
		// pointer," which is the property under test.
		assert.Nil(t, provider, "a broken binding must never resolve to the instance provider")
		assert.Empty(t, apiBase)
	})

	t.Run("empty binding resolves to the instance provider", func(t *testing.T) {
		projects := fakeProjectGetter{cfg: &board.ProjectConfig{Name: "proj", GitHubCredential: ""}}
		resolve := newProviderForProject(projects, authSvc, instanceProvider, instanceAPIBase)

		provider, apiBase, err := resolve(context.Background(), "proj")

		require.NoError(t, err)
		assert.Same(t, instanceProvider, provider)
		assert.Equal(t, instanceAPIBase, apiBase)
	})

	t.Run("nil auth service resolves to the instance provider despite a binding", func(t *testing.T) {
		projects := fakeProjectGetter{cfg: &board.ProjectConfig{Name: "proj", GitHubCredential: "good-cred"}}
		resolve := newProviderForProject(projects, nil, instanceProvider, instanceAPIBase)

		var (
			provider githubauth.TokenGenerator
			apiBase  string
			err      error
		)

		require.NotPanics(t, func() {
			provider, apiBase, err = resolve(context.Background(), "proj")
		}, "nil auth service must never be dereferenced")
		require.NoError(t, err)
		assert.Same(t, instanceProvider, provider,
			"the none-mode OR-arm must decide here, not the empty-binding arm")
		assert.Equal(t, instanceAPIBase, apiBase)
	})

	t.Run("healthy binding resolves to the pool provider", func(t *testing.T) {
		projects := fakeProjectGetter{cfg: &board.ProjectConfig{Name: "proj", GitHubCredential: "good-cred"}}
		resolve := newProviderForProject(projects, authSvc, instanceProvider, instanceAPIBase)

		provider, apiBase, err := resolve(context.Background(), "proj")

		require.NoError(t, err)
		require.NotNil(t, provider)
		assert.NotSame(t, instanceProvider, provider, "a healthy binding must resolve to the pool provider")
		assert.NotEqual(t, instanceAPIBase, apiBase)
	})

	t.Run("GetProject error is propagated and fails closed", func(t *testing.T) {
		wantErr := errors.New("project lookup boom")
		projects := fakeProjectGetter{err: wantErr}
		resolve := newProviderForProject(projects, authSvc, instanceProvider, instanceAPIBase)

		provider, apiBase, err := resolve(context.Background(), "proj")

		require.ErrorIs(t, err, wantErr)
		assert.Nil(t, provider)
		assert.Empty(t, apiBase)
	})
}
