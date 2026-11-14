package api

import (
	"context"
	"fmt"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/sentinel"
)

// runKindBase fills a run kind's common payload fields for a card: the
// project's repository and worker image, the card's base branch, and a
// contents-read token minted from the project's binding, failing closed as a
// launch does. A model kind also gets launch's model choice and its mode. It
// writes nothing to the card.
func (h *backendHandlers) runKindBase(ctx context.Context, project, cardID string, model bool) (protocol.RunKindPayload, llmchannel.Mode, error) {
	var none protocol.RunKindPayload

	card, err := h.svc.GetCard(ctx, project, cardID)
	if err != nil {
		return none, "", fmt.Errorf("read card: %w", err)
	}

	projectCfg, err := h.svc.GetProject(ctx, project)
	if err != nil {
		return none, "", fmt.Errorf("read project: %w", err)
	}

	provider, _, err := h.providerForProject(ctx, project)
	if err != nil {
		return none, "", fmt.Errorf("project credential unavailable: %w", err)
	}

	repoName, _ := repoNameFromURL(projectCfg.Repo)

	// The run clones once at start, so the token's expiry is not sent.
	token, _, err := provider.GenerateRepoReadToken(ctx, repoName)
	if err != nil {
		return none, "", fmt.Errorf("mint a read token: %w", err)
	}

	p := protocol.RunKindPayload{
		CardID: card.ID, Project: project, RepoURL: projectCfg.Repo, BaseBranch: card.BaseBranch, GitToken: token,
	}

	if projectCfg.RemoteExecution != nil {
		p.WorkerImage = projectCfg.RemoteExecution.WorkerImage
	}

	if !model {
		return p, "", nil
	}

	choice := h.chooseModel(ctx, project, card.ID, projectCfg)
	p.Model, p.Selection, p.LLMEndpoint = choice.model, choice.selection, choice.endpoint

	return p, choice.mode, nil
}

// sentinelRelaunch is Sentinel's rerun of a card outside a playbook run: a
// hand run's checks and launch, recorded with origin sentinel, which keeps the
// card's heal attempts.
func (h *backendHandlers) sentinelRelaunch(ctx context.Context, project, id string) error {
	err := h.handRunPreflight(ctx, project, id)
	if err == nil {
		_, err = h.launch(ctx, project, id, launchOptions{handRun: true, origin: sentinel.OriginSentinel})
	}

	return committableLaunchError(ctx, project, id, err)
}
