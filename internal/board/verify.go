package board

// VerifyConfig is an operator-declared verify gate for a task run. Command is a
// shell string the agent runs via bash -c; TimeoutSeconds bounds the run (0 =
// agent default) and applies to detected/proposed commands too; Env lists
// passthrough environment variable names only, never values. Declared on a
// project (the default for its cards) and optionally overridden per card. CM
// resolves card-over-project field-by-field before a run (see ResolveVerify).
type VerifyConfig struct {
	Command        string   `yaml:"command,omitempty"         json:"command,omitempty"`
	TimeoutSeconds int      `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	Env            []string `yaml:"env,omitempty"             json:"env,omitempty"`
}

// IsZero reports whether the config carries no operator intent and can be
// dropped so .board.yaml and card frontmatter stay clean.
func (v *VerifyConfig) IsZero() bool {
	return v == nil || (v.Command == "" && v.TimeoutSeconds == 0 && len(v.Env) == 0)
}

// MarshalYAML emits env whenever it is non-nil - including an explicit empty
// list - so a card's "override to clear env" (env: []) survives the YAML
// round-trip distinguishably from an absent env (inherit the project's). A
// struct with `yaml:"env,omitempty"` cannot express this: omitempty drops a
// non-nil empty slice, collapsing override-to-clear into inherit on reload.
// Unmarshal needs no custom hook - the default already reads `env: []` as a
// non-nil empty slice and an absent key as nil.
func (v VerifyConfig) MarshalYAML() (any, error) {
	out := map[string]any{}

	if v.Command != "" {
		out["command"] = v.Command
	}

	if v.TimeoutSeconds != 0 {
		out["timeout_seconds"] = v.TimeoutSeconds
	}

	if v.Env != nil {
		out["env"] = v.Env
	}

	return out, nil
}

// ResolveVerify merges a card's verify config over its project's, field by
// field: Command is the card's when non-empty else the project's;
// TimeoutSeconds is the card's when > 0 else the project's; Env is the card's
// when non-nil else the project's. Returns nil when nothing resolves, so
// consumers treat that as "nothing declared" and fall back to their own
// detection. Never mutates either input.
func ResolveVerify(card, project *VerifyConfig) *VerifyConfig {
	var c, p VerifyConfig
	if card != nil {
		c = *card
	}

	if project != nil {
		p = *project
	}

	out := VerifyConfig{
		Command:        c.Command,
		TimeoutSeconds: c.TimeoutSeconds,
		Env:            c.Env,
	}

	if out.Command == "" {
		out.Command = p.Command
	}

	if out.TimeoutSeconds <= 0 {
		out.TimeoutSeconds = p.TimeoutSeconds
	}

	if out.Env == nil {
		out.Env = p.Env
	}

	if out.IsZero() {
		return nil
	}

	return &out
}
