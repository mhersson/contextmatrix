package main

import (
	"context"

	githubauth "github.com/mhersson/contextmatrix-githubauth"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/board"
)

// projectGetter is the narrow slice of *service.CardService that
// newProviderForProject needs. Defined here, in the consuming package, per
// project convention (interfaces belong where they're used) - it exists so
// the resolution logic below can be built and tested without depending on
// the concrete service type.
type projectGetter interface {
	GetProject(ctx context.Context, name string) (*board.ProjectConfig, error)
}

// newProviderForProject builds the resolver wired into
// RouterConfig.ProviderForProject and reused by the GitHub-issue-sync path:
// the project's credential binding when set (fail-closed on a broken one),
// else the instance-wide provider. instanceProvider/instanceAPIBase are the
// instance-wide fallback; authSvc may be nil (auth.mode "none" - the OR
// guard below short-circuits before ever dereferencing it).
// provider_test.go covers every branch directly.
func newProviderForProject(
	projects projectGetter,
	authSvc *auth.Service,
	instanceProvider githubauth.TokenGenerator,
	instanceAPIBase string,
) func(ctx context.Context, project string) (githubauth.TokenGenerator, string, error) {
	return func(ctx context.Context, project string) (githubauth.TokenGenerator, string, error) {
		pcfg, err := projects.GetProject(ctx, project)
		if err != nil {
			return nil, "", err
		}

		if pcfg.GitHubCredential == "" || authSvc == nil {
			return instanceProvider, instanceAPIBase, nil // instance credential - pre-binding behavior
		}

		provider, apiBase, _, err := authSvc.TokenProviderFor(ctx, pcfg.GitHubCredential)
		if err != nil {
			return nil, "", err // FAIL CLOSED: broken binding never falls back
		}

		return provider, apiBase, nil
	}
}
