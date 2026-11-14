package api

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/mhersson/contextmatrix/internal/github"
	"github.com/mhersson/contextmatrix/internal/githubauth"
	"github.com/mhersson/contextmatrix/internal/sentinel"
)

// sentinelGitHub reads a project's standing guidance PR with the project's own
// credential.
type sentinelGitHub struct {
	providerForProject func(ctx context.Context, project string) (githubauth.TokenGenerator, string, error)
}

var _ sentinel.GitHub = (*sentinelGitHub)(nil)

func (g *sentinelGitHub) client(ctx context.Context, project string) (*github.Client, error) {
	provider, apiBaseURL, err := g.providerForProject(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("project credential unavailable: %w", err)
	}

	return github.NewClientWithBaseURL(provider, apiBaseURL), nil
}

func (g *sentinelGitHub) PullState(ctx context.Context, project, prURL string) (sentinel.PullState, error) {
	owner, repo, number, err := parsePullURL(prURL)
	if err != nil {
		return sentinel.PullState{}, err
	}

	c, err := g.client(ctx, project)
	if err != nil {
		return sentinel.PullState{}, err
	}

	pr, err := c.FetchPull(ctx, owner, repo, number)
	if err != nil {
		return sentinel.PullState{}, fmt.Errorf("read pull request: %w", err)
	}

	return sentinel.PullState{
		Open: pr.State == "open", Merged: pr.Merged, HeadSHA: pr.HeadSHA, MergeSHA: pr.MergeCommitSHA,
	}, nil
}

func (g *sentinelGitHub) FileAt(ctx context.Context, project, prURL, path, ref string) (string, bool, error) {
	owner, repo, _, err := parsePullURL(prURL)
	if err != nil {
		return "", false, err
	}

	c, err := g.client(ctx, project)
	if err != nil {
		return "", false, err
	}

	content, err := c.FetchFile(ctx, owner, repo, path, ref)
	if errors.Is(err, github.ErrNotFound) {
		return "", false, nil
	}

	if err != nil {
		return "", false, fmt.Errorf("read file: %w", err)
	}

	return content, true, nil
}

// parsePullURL splits a pull request URL path /{owner}/{repo}/pull/{n}.
func parsePullURL(prURL string) (owner, repo string, number int, err error) {
	u, err := url.Parse(prURL)
	if err != nil {
		return "", "", 0, fmt.Errorf("parse pull request url: %w", err)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 4 || parts[2] != "pull" || parts[0] == "" || parts[1] == "" {
		return "", "", 0, fmt.Errorf("not a pull request url: %q", prURL)
	}

	number, err = strconv.Atoi(parts[3])
	if err != nil || number <= 0 {
		return "", "", 0, fmt.Errorf("not a pull request url: %q", prURL)
	}

	return parts[0], parts[1], number, nil
}
