// Package boardmerge implements three-way merges for the files ContextMatrix
// stores in a boards repository. It is pure: no git, no filesystem. The
// gitsync layer feeds it the index stages of each unmerged path and writes
// what it returns.
package boardmerge

import (
	"regexp"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
)

type Kind int

const (
	KindOther    Kind = iota
	KindCard          // <project>/tasks/<ID>.md
	KindProject       // <project>/.board.yaml
	KindPlaybook      // playbooks/<id>.yaml
)

// Input carries the three stages of one unmerged path from the git index.
type Input struct {
	Path   string
	Base   []byte // nil: no common ancestor
	Ours   []byte // nil: deleted on our side
	Theirs []byte // nil: deleted on their side
}

// File is an extra file the resolver wants written and staged alongside the
// resolved path (e.g. a re-minted local card).
type File struct {
	Path    string
	Content []byte
}

// Resolution records how one path's conflict was resolved, for the merge
// commit's audit trail.
type Resolution struct {
	Path   string `json:"path"`
	CardID string `json:"card_id,omitempty"`
	Rule   string `json:"rule"`
	Detail string `json:"detail,omitempty"`
	OldID  string `json:"old_id,omitempty"`
	NewID  string `json:"new_id,omitempty"`
}

// Output is the result of resolving one path.
type Output struct {
	Content     []byte
	Deleted     bool
	Extra       []File            // extra files to write and stage (re-minted local card)
	Renames     map[string]string // "<project>/<OldID>" -> NewID
	Resolutions []Resolution
}

// Context carries the state Resolve needs beyond the raw three-way input:
// instance identity, timing, and callbacks into the board/service layer.
type Context struct {
	Instance       string
	OursCommit     string
	TheirsCommit   string
	Now            func() time.Time
	MergeBody      func(base, ours, theirs string) (merged string, clean bool)
	Project        func(project string) (*board.ProjectConfig, error)
	MintID         func(project string) (string, error)
	CardExists     func(project, id string) bool
	PlaybookExists func(id string) bool
	Renames        map[string]string // applied to references added on our side
}

const (
	RuleFieldMerge        = "card.field_merge" // conflicted file merged by field rules with no override
	RuleLaterUpdated      = "scalar.later_updated"
	RuleTerminalWins      = "state.terminal_wins"
	RuleBodyLaterUpdated  = "body.later_updated"
	RuleAddAddRemint      = "add_add.remint"
	RuleSourceDedupe      = "add_add.source_dedupe"
	RuleDeleteWins        = "delete_wins"
	RuleTheirsOther       = "other.theirs_wins"
	RuleUnparseable       = "unparseable.take_parsing"
	RuleInvariantFallback = "invariant.take_theirs"
	RuleInvariantRepair   = "invariant.repair"
	RuleProjectImmutable  = "project.immutable_theirs"
	RulePlaybookRemint    = "playbook.entry_remint"
	RulePlaybookReslug    = "playbook.reslug"
	RulePlaybookDedupe    = "playbook.entry_dedupe"
	RuleEpochWins         = "claim.epoch_wins"          // the higher claim_epoch supplied the claim tuple
	RuleTerminalOverStall = "claim.terminal_over_stall" // a bare stall at a higher epoch lost to a completion
	RuleDoubleClaim       = "claim.double_claim"        // both sides claimed from empty; earlier claimed_at won
	RuleActiveOverRelease = "claim.active_over_release" // at equal raised epochs an active claim beat an emptied tuple
)

// cardPath matches what the index loader accepts as a card: any .md file
// directly under a project's tasks directory. The id part is deliberately not
// restricted to an uppercase prefix - nothing in ContextMatrix validates the
// shape of a project prefix, so a card whose id came from a lowercase or
// digit-leading prefix is a real card the loader serves. A pattern that
// missed it would classify it as an ordinary file, dropping the local side of
// every conflict on it with no re-mint. Only the trailing -<digits> that
// GenerateCardID always appends is required, and a leading dot is excluded so
// the store's own .tmp- files never look like cards.
var (
	cardPath     = regexp.MustCompile(`^([^/]+)/tasks/([^./][^/]*-[0-9]+)\.md$`)
	playbookPath = regexp.MustCompile(`^playbooks/([^/]+)\.yaml$`)
)

// Classify returns the kind of a repo-relative path plus its project and
// id parts (project empty for playbooks; id is the card ID or playbook id).
func Classify(path string) (kind Kind, project, id string) {
	if m := cardPath.FindStringSubmatch(path); m != nil {
		return KindCard, m[1], m[2]
	}

	if strings.HasSuffix(path, "/.board.yaml") && strings.Count(path, "/") == 1 {
		return KindProject, strings.TrimSuffix(path, "/.board.yaml"), ""
	}

	if m := playbookPath.FindStringSubmatch(path); m != nil {
		return KindPlaybook, "", m[1]
	}

	return KindOther, "", ""
}

// Resolve merges one unmerged path according to its kind.
func Resolve(in Input, c Context) (Output, error) {
	if c.Now == nil {
		c.Now = time.Now
	}

	kind, project, id := Classify(in.Path)

	switch kind {
	case KindCard:
		return resolveCard(in, project, id, c)
	case KindProject:
		return resolveProject(in, project, c)
	case KindPlaybook:
		return resolvePlaybook(in, id, c)
	default:
		return resolveOther(in), nil
	}
}

func resolveOther(in Input) Output {
	res := Resolution{Path: in.Path, Rule: RuleTheirsOther}
	if in.Theirs == nil {
		res.Detail = "deleted on remote"

		return Output{Deleted: true, Resolutions: []Resolution{res}}
	}

	return Output{Content: in.Theirs, Resolutions: []Resolution{res}}
}
