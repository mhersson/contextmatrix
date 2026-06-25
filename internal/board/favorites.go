package board

import "gopkg.in/yaml.v3"

// TierFavorites holds operator-preferred model slugs for one complexity tier.
// All applies to every role; ByRole narrows to a specific role (coder/reviewer).
// The YAML accepts either a bare list (-> All) or a role map.
type TierFavorites struct {
	All    []string
	ByRole map[string][]string
}

// UnmarshalYAML accepts `tier: [m1, m2]` or `tier: {reviewer: [m]}`.
func (t *TierFavorites) UnmarshalYAML(value *yaml.Node) error {
	var list []string
	if err := value.Decode(&list); err == nil {
		t.All = list

		return nil
	}

	var byRole map[string][]string
	if err := value.Decode(&byRole); err != nil {
		return err
	}

	t.ByRole = byRole

	return nil
}
