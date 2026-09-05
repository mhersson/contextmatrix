package board

import "slices"

// CardDefaults is a project's create-time default for the human-set
// automation fields on a card. A nil *CardDefaults on ProjectConfig means the
// built-in set: everything off except create_pr. When the block is present
// every field is explicit, with one exception: CreatePR nil keeps the built-in
// true so a hand-written block that omits it does not silently switch pull
// requests off. Only an explicit false is ever persisted (see Normalize).
//
// Applied by the service when a top-level card is created with a field left
// unset; subtasks never inherit - their PR and run configuration belong to the
// parent card whose branch carries the work. Best-of-N is deliberately not a
// project default.
type CardDefaults struct {
	Autonomous         bool     `yaml:"autonomous,omitempty"           json:"autonomous"`
	MaxCapability      bool     `yaml:"max_capability,omitempty"       json:"max_capability"`
	MobParticipants    int      `yaml:"mob_participants,omitempty"     json:"mob_participants"`
	MobPhases          []string `yaml:"mob_phases,omitempty"           json:"mob_phases,omitempty"`
	CreatePR           *bool    `yaml:"create_pr,omitempty"            json:"create_pr,omitempty"`
	AwaitCI            bool     `yaml:"await_ci,omitempty"             json:"await_ci"`
	AwaitCopilotReview bool     `yaml:"await_copilot_review,omitempty" json:"await_copilot_review"`
}

// CreatePROn resolves the nullable CreatePR: nil (or a nil receiver) is the
// built-in true.
func (d *CardDefaults) CreatePROn() bool {
	return d == nil || d.CreatePR == nil || *d.CreatePR
}

// ResolveCardDefaults returns a project's effective create-time defaults:
// the built-in set when d is nil, otherwise d with CreatePR made concrete and
// MobPhases cloned so callers never alias the stored config.
func ResolveCardDefaults(d *CardDefaults) CardDefaults {
	out := CardDefaults{}
	if d != nil {
		out = *d
		out.MobPhases = slices.Clone(d.MobPhases)
	}

	on := d.CreatePROn()
	out.CreatePR = &on

	return out
}

// Normalize returns a copy with built-in values dropped - create_pr true
// becomes nil, phases without seats are removed - or nil when nothing but
// built-ins remains, so .board.yaml carries only real operator intent. The
// receiver is not modified.
func (d *CardDefaults) Normalize() *CardDefaults {
	if d == nil {
		return nil
	}

	n := *d
	n.MobPhases = slices.Clone(d.MobPhases)

	if n.CreatePR != nil {
		if *n.CreatePR {
			n.CreatePR = nil
		} else {
			n.CreatePR = new(false)
		}
	}

	if n.MobParticipants == 0 || len(n.MobPhases) == 0 {
		n.MobPhases = nil
	}

	if !n.Autonomous && !n.MaxCapability && n.MobParticipants == 0 &&
		n.MobPhases == nil && n.CreatePR == nil && !n.AwaitCI && !n.AwaitCopilotReview {
		return nil
	}

	return &n
}
