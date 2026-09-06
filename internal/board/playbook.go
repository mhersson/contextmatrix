package board

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Playbook entry types. Card entries dispatch to agents when the playbook
// runs; manual entries pause the run for a human.
const (
	EntryTypeCard   = "card"
	EntryTypeManual = "manual"
)

// Playbook run statuses. running and waiting are "active": the playbook's
// cards are locked against hand runs and a second Play is refused.
const (
	RunStatusRunning   = "running"
	RunStatusWaiting   = "waiting"
	RunStatusStopped   = "stopped"
	RunStatusCompleted = "completed"
)

// ValidPlaybookRunStatus reports whether s is one of the run statuses.
func ValidPlaybookRunStatus(s string) bool {
	switch s {
	case RunStatusRunning, RunStatusWaiting, RunStatusStopped, RunStatusCompleted:
		return true
	}

	return false
}

// PlaybookRun is the state of a playbook's current or last run. It is
// present once a run has started and is owned by the instance that started
// it (Instance is empty on a private board). Entry names the entry the run
// is on, empty once completed; Reason explains a waiting status to a human.
type PlaybookRun struct {
	Status    string     `yaml:"status"               json:"status"`
	Instance  string     `yaml:"instance,omitempty"   json:"instance,omitempty"`
	StartedBy string     `yaml:"started_by,omitempty" json:"started_by,omitempty"`
	StartedAt time.Time  `yaml:"started_at"           json:"started_at"`
	UpdatedAt time.Time  `yaml:"updated_at"           json:"updated_at"`
	Entry     string     `yaml:"entry,omitempty"      json:"entry,omitempty"`
	Reason    string     `yaml:"reason,omitempty"     json:"reason,omitempty"`
	EndedAt   *time.Time `yaml:"ended_at,omitempty"   json:"ended_at,omitempty"`
}

// Active reports whether the run is running or waiting. Nil-safe.
func (r *PlaybookRun) Active() bool {
	return r != nil && (r.Status == RunStatusRunning || r.Status == RunStatusWaiting)
}

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
	// Runnable marks a playbook whose cards carry the forced settings and
	// which a human may Play. BaseBranch is the branch the playbook branch
	// is cut from on its first run; empty means the repository default.
	Runnable   bool         `yaml:"runnable,omitempty"    json:"runnable"`
	BaseBranch string       `yaml:"base_branch,omitempty" json:"base_branch,omitempty"`
	Run        *PlaybookRun `yaml:"run,omitempty"         json:"run,omitempty"`
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

	if p.Run != nil {
		if !p.Runnable {
			return fmt.Errorf("%w: run state on a playbook that is not runnable", ErrInvalidPlaybook)
		}

		if !ValidPlaybookRunStatus(p.Run.Status) {
			return fmt.Errorf("%w: unknown run status %q", ErrInvalidPlaybook, p.Run.Status)
		}

		if p.Run.Entry != "" {
			if _, ok := seenIDs[p.Run.Entry]; !ok {
				return fmt.Errorf("%w: run entry %q does not exist", ErrInvalidPlaybook, p.Run.Entry)
			}
		}
	}

	return nil
}

// Branch is the shared branch every card of a runnable playbook targets.
// The id never changes, so the name is stable for the playbook's life.
func (p *Playbook) Branch() string {
	return "playbook/" + p.ID
}

// RunActive reports whether the playbook has a running or waiting run.
func (p *Playbook) RunActive() bool {
	return p.Run.Active()
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
