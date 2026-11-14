// Package attention merges everything waiting on a person into one list.
package attention

import (
	"cmp"
	"slices"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/sentinel"
	"github.com/mhersson/contextmatrix/internal/service"
)

// Item kinds.
const (
	KindBlueprint = "blueprint"
	KindPlaybook  = "playbook"
	KindCard      = "card"
	KindSentinel  = "sentinel"
)

// Item is one thing waiting on a person.
type Item struct {
	Kind     string       `json:"kind"`
	ID       string       `json:"id"` // session id, playbook id or card id
	Project  string       `json:"project,omitempty"`
	Title    string       `json:"title"`
	Reason   string       `json:"reason"`
	Since    time.Time    `json:"since"`
	Mine     bool         `json:"mine,omitempty"`
	Playbook *PlaybookRef `json:"playbook,omitempty"` // the waiting run a card item holds
}

// PlaybookRef names the waiting playbook run a card item holds.
type PlaybookRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// CardKey identifies a card across projects.
type CardKey struct{ Project, ID string }

// BlueprintWait is an architect session waiting on its owner.
type BlueprintWait struct {
	ID, Title, Owner, Kind string
	Since                  time.Time
}

// Snapshot is everything Merge reads.
type Snapshot struct {
	Blueprints []BlueprintWait
	Playbooks  []*service.PlaybookDetail // runs whose status is waiting
	Cards      map[CardKey]*board.Card   // worker-parked, held, and the waiting runs' entry cards
	Holds      map[CardKey]sentinel.CardHold
}

func blueprintReason(kind string) string {
	switch kind {
	case "question":
		return "The architect asks a question"
	case "round":
		return "The architect asks a round of questions"
	case "gate":
		return "The architect waits for your sign-off"
	case "mockup":
		return "The architect waits for your mockup review"
	}

	return "The architect waits for you"
}

// Merge yields one item per stuck card, plus blueprint and playbook waits,
// newest first. viewer is the identity whose waits are marked Mine; an empty
// viewer owns nothing.
func Merge(s Snapshot, viewer string) []Item {
	var items []Item

	cardItems := make(map[CardKey]int)

	for k, c := range s.Cards {
		hold, held := s.Holds[k]
		if it, ok := cardItem(c, hold, held); ok {
			cardItems[k] = len(items)
			items = append(items, it)
		}
	}

	pbs := slices.SortedFunc(slices.Values(s.Playbooks), func(a, b *service.PlaybookDetail) int {
		return cmp.Compare(a.ID, b.ID)
	})

	for _, pb := range pbs {
		if pb.Run == nil {
			continue
		}

		if k, ok := entryCard(pb); ok {
			if i, isItem := cardItems[k]; isItem {
				if items[i].Playbook == nil {
					items[i].Playbook = &PlaybookRef{ID: pb.ID, Title: pb.Title}
				}

				continue
			}

			if h, held := s.Holds[k]; held && h.Healing {
				continue
			}
		}

		items = append(items, Item{
			Kind: KindPlaybook, ID: pb.ID, Title: pb.Title, Reason: pb.Run.Reason,
			Since: pb.Run.UpdatedAt, Mine: viewer != "" && pb.Run.StartedBy == viewer,
		})
	}

	for _, b := range s.Blueprints {
		items = append(items, Item{
			Kind: KindBlueprint, ID: b.ID, Title: b.Title, Reason: blueprintReason(b.Kind),
			Since: b.Since, Mine: viewer != "" && b.Owner == viewer,
		})
	}

	slices.SortFunc(items, func(a, b Item) int {
		return cmp.Or(b.Since.Compare(a.Since), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Project, b.Project), cmp.Compare(a.ID, b.ID))
	})

	return items
}

// cardItem applies the per-card rules; held says the card has a hold at all.
func cardItem(c *board.Card, hold sentinel.CardHold, held bool) (Item, bool) {
	if board.IsTerminalState(c.State) {
		return Item{}, false
	}

	base := Item{ID: c.ID, Project: c.Project, Title: c.Title}

	switch {
	case held && hold.Healing:
		return Item{}, false
	case held:
		base.Kind, base.Reason, base.Since = KindSentinel, hold.Reason, hold.Since

		return base, true
	case c.WorkerStatus == "parked":
		base.Kind, base.Reason, base.Since = KindCard, "", c.Updated

		for i := len(c.ActivityLog) - 1; i >= 0; i-- {
			if e := c.ActivityLog[i]; e.Action == "parked" {
				base.Reason, base.Since = e.Message, e.Timestamp

				break
			}
		}

		return base, true
	}

	return Item{}, false
}

// entryCard resolves the card entry a waiting run is on.
func entryCard(pb *service.PlaybookDetail) (CardKey, bool) {
	for _, e := range pb.Entries {
		if e.ID == pb.Run.Entry && e.Type == board.EntryTypeCard {
			return CardKey{Project: e.Project, ID: e.Card}, true
		}
	}

	return CardKey{}, false
}
