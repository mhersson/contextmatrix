package sentinel

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/service"
)

const (
	frictionLabel           = "sentinel-friction"
	maxFrictionCardsPerDay  = 5
	maxFrictionTitle        = 200
	limitReason             = "today's friction-card limit reached"
	secretReason            = "holds a secret"
	rerouteReason           = "re-routed: mentions CM"
	blankReason             = "has no text"
	likeFindingReason       = "like finding "
	likeCardReason          = "like card "
	frictionDefaultType     = "task"
	frictionDefaultPriority = "medium"
)

// cmTerms name CM itself; guidance holds project knowledge only.
var cmTerms = regexp.MustCompile(`(?i)\b(?:Sentinel|ContextMatrix|worker image|agent run|the worker)\b`)

// holdsSecretFinding reports whether any part of the finding holds a secret.
func holdsSecretFinding(f state.Finding, secrets []string) bool {
	return holdsSecret(f.Title+"\n"+f.Text+"\n"+f.Evidence, secrets)
}

// routeGuidance decides a queued guidance finding: dropped when it holds a
// secret or has no text, re-routed to an admin project_verify note when its
// title or text names CM, a duplicate of an earlier finding already pending or
// in a guidance PR, else pending. earlier may hold findings in any state.
func routeGuidance(f state.Finding, earlier []state.Finding, secrets []string) (newKind, newState, reason string) {
	switch {
	case holdsSecretFinding(f, secrets):
		return f.Kind, state.FindingDropped, secretReason
	case strings.TrimSpace(f.Text) == "":
		return f.Kind, state.FindingDropped, blankReason
	case cmTerms.MatchString(f.Title + "\n" + f.Text):
		return protocol.FindingProjectVerify, state.FindingNote, rerouteReason
	}

	for _, e := range earlier {
		switch e.State {
		case state.FindingPending, state.FindingProposed, state.FindingMerged, state.FindingRejected:
			if similar(f.Text, e.Text) {
				return f.Kind, state.FindingDuplicate, likeFindingReason + strconv.FormatInt(e.ID, 10)
			}
		}
	}

	return f.Kind, state.FindingPending, ""
}

// routeCode decides a queued code finding against the project's friction
// cards. A card in not_planned counts as seen: it was rejected. carded means
// the caller creates the card; queued keeps the finding for a later day.
func routeCode(f state.Finding, cards []*board.Card, secrets []string, createdToday int) (newState, reason string) {
	if holdsSecretFinding(f, secrets) {
		return state.FindingDropped, secretReason
	}

	title := frictionTitle(f)

	for _, c := range cards {
		if c.State == board.StateDone || !slices.Contains(c.Labels, frictionLabel) {
			continue
		}

		if slices.Equal(words(title), words(c.Title)) || similar(title+" "+f.Text, c.Title+" "+c.Body) {
			return state.FindingDuplicate, likeCardReason + c.ID
		}
	}

	if createdToday >= maxFrictionCardsPerDay {
		return state.FindingQueued, limitReason
	}

	return state.FindingCarded, ""
}

// frictionTitle is the finding's title, else the first line of its text.
func frictionTitle(f state.Finding) string {
	if t := strings.TrimSpace(f.Title); t != "" {
		return t
	}

	return firstLine(f.Text, maxFrictionTitle)
}

// frictionCardInput is the new card a code finding becomes; it is never
// autonomous.
func frictionCardInput(f state.Finding, p *board.ProjectConfig) service.CreateCardInput {
	notAutonomous := false

	return service.CreateCardInput{
		Title:      frictionTitle(f),
		Type:       listedOrFirst(p.Types, frictionDefaultType),
		Priority:   listedOrFirst(p.Priorities, frictionDefaultPriority),
		Labels:     []string{frictionLabel},
		Autonomous: &notAutonomous,
		Body:       f.Text + "\n\n## Evidence\n\n" + f.Evidence,
		Log: &board.ActivityEntry{
			Agent: Agent, Action: activityAction,
			Message: "Sentinel created this card from the friction a run of " + f.CardID + " showed",
		},
	}
}

// listedOrFirst is want when the project lists it, else the project's first
// entry, else want.
func listedOrFirst(listed []string, want string) string {
	switch {
	case slices.Contains(listed, want), len(listed) == 0:
		return want
	default:
		return listed[0]
	}
}
