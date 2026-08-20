package board

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Playbook entry types. A future runnable version attaches semantics per
// type: card entries dispatch to agents, manual entries pause for a human.
const (
	EntryTypeCard   = "card"
	EntryTypeManual = "manual"
)

// ErrInvalidPlaybook is the sentinel wrapped by all playbook validation
// failures.
var ErrInvalidPlaybook = errors.New("invalid playbook")

var (
	playbookIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	playbookNonAlphanum = regexp.MustCompile(`[^a-z0-9]+`)
)

// Playbook is an ordered cross-project list of steps. Order is array order;
// no other ordering state exists.
type Playbook struct {
	ID          string          `yaml:"id"                    json:"id"`
	Title       string          `yaml:"title"                 json:"title"`
	Description string          `yaml:"description,omitempty" json:"description,omitempty"`
	CreatedBy   string          `yaml:"created_by,omitempty"  json:"created_by,omitempty"`
	Created     time.Time       `yaml:"created_at"            json:"created_at"`
	Updated     time.Time       `yaml:"updated_at"            json:"updated_at"`
	NextEntryID int             `yaml:"next_entry_id"         json:"next_entry_id"`
	Entries     []PlaybookEntry `yaml:"entries"               json:"entries"`
}

// PlaybookEntry is one step: a reference to a card in some project, or a
// manual gate step. Note is a human-only channel, contractually excluded
// from any future agent-facing context.
type PlaybookEntry struct {
	ID      string     `yaml:"id"                json:"id"`
	Type    string     `yaml:"type"              json:"type"`
	Project string     `yaml:"project,omitempty" json:"project,omitempty"`
	Card    string     `yaml:"card,omitempty"    json:"card,omitempty"`
	Text    string     `yaml:"text,omitempty"    json:"text,omitempty"`
	Done    bool       `yaml:"done,omitempty"    json:"done,omitempty"`
	DoneBy  string     `yaml:"done_by,omitempty" json:"done_by,omitempty"`
	DoneAt  *time.Time `yaml:"done_at,omitempty" json:"done_at,omitempty"`
	Note    string     `yaml:"note,omitempty"    json:"note,omitempty"`
}

// ParsePlaybook decodes a playbook YAML file. Unknown fields are ignored so
// older binaries tolerate files written by newer ones.
func ParsePlaybook(data []byte) (*Playbook, error) {
	var p Playbook
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse playbook: %w", err)
	}

	return &p, nil
}

// SerializePlaybook encodes a playbook as YAML.
func SerializePlaybook(p *Playbook) ([]byte, error) {
	data, err := yaml.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal playbook: %w", err)
	}

	return data, nil
}

// Validate checks structural invariants. It does not check card existence -
// that is the service layer's job (it needs the card store).
func (p *Playbook) Validate() error {
	if strings.TrimSpace(p.Title) == "" {
		return fmt.Errorf("%w: title is required", ErrInvalidPlaybook)
	}

	if !playbookIDPattern.MatchString(p.ID) {
		return fmt.Errorf("%w: id %q must match [a-z0-9][a-z0-9-]*", ErrInvalidPlaybook, p.ID)
	}

	seenIDs := make(map[string]struct{}, len(p.Entries))
	seenCards := make(map[string]struct{}, len(p.Entries))

	for i := range p.Entries {
		e := &p.Entries[i]
		if _, dup := seenIDs[e.ID]; dup {
			return fmt.Errorf("%w: duplicate entry id %q", ErrInvalidPlaybook, e.ID)
		}

		seenIDs[e.ID] = struct{}{}

		switch e.Type {
		case EntryTypeCard:
			if e.Project == "" || e.Card == "" {
				return fmt.Errorf("%w: card entry %q needs project and card", ErrInvalidPlaybook, e.ID)
			}

			if e.Text != "" {
				return fmt.Errorf("%w: card entry %q must not carry text", ErrInvalidPlaybook, e.ID)
			}

			key := e.Project + "/" + e.Card
			if _, dup := seenCards[key]; dup {
				return fmt.Errorf("%w: duplicate card entry %s", ErrInvalidPlaybook, key)
			}

			seenCards[key] = struct{}{}
		case EntryTypeManual:
			if strings.TrimSpace(e.Text) == "" {
				return fmt.Errorf("%w: manual entry %q needs text", ErrInvalidPlaybook, e.ID)
			}

			if e.Project != "" || e.Card != "" {
				return fmt.Errorf("%w: manual entry %q must not reference a card", ErrInvalidPlaybook, e.ID)
			}
		default:
			return fmt.Errorf("%w: entry %q has unknown type %q", ErrInvalidPlaybook, e.ID, e.Type)
		}
	}

	return nil
}

// FindEntry returns the index of the entry with the given ID, or -1.
func (p *Playbook) FindEntry(entryID string) int {
	for i := range p.Entries {
		if p.Entries[i].ID == entryID {
			return i
		}
	}

	return -1
}

// HasCardEntry reports whether the playbook already references the card.
func (p *Playbook) HasCardEntry(project, card string) bool {
	for i := range p.Entries {
		e := &p.Entries[i]
		if e.Type == EntryTypeCard && e.Project == project && e.Card == card {
			return true
		}
	}

	return false
}

// maxSlugLength caps the derived slug so it can never produce a filename
// beyond the filesystem's NAME_MAX; atomicWriteFile fails with ENAMETOOLONG
// on longer names, surfacing as a 500 on POST /api/playbooks.
const maxSlugLength = 100

// SlugifyPlaybookTitle derives a playbook ID from a title. Falls back to
// "playbook" when nothing usable remains; the service uniquifies collisions
// with a numeric suffix.
func SlugifyPlaybookTitle(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = playbookNonAlphanum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")

	if len(s) > maxSlugLength {
		s = strings.TrimRight(s[:maxSlugLength], "-")
	}

	if s == "" {
		return "playbook"
	}

	return s
}
