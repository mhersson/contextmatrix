package boardmerge

import "slices"

// pick returns the three-way choice for a scalar. conflict is true when both
// sides changed to different values; the caller then applies a tiebreak.
func pick[T comparable](base, ours, theirs T) (T, bool) {
	switch {
	case ours == theirs:
		return ours, false
	case ours == base:
		return theirs, false
	case theirs == base:
		return ours, false
	default:
		return ours, true
	}
}

// pickEq is pick for values compared with a custom equality (pointers, structs).
func pickEq[T any](base, ours, theirs T, eq func(a, b T) bool) (T, bool) {
	switch {
	case eq(ours, theirs):
		return ours, false
	case eq(ours, base):
		return theirs, false
	case eq(theirs, base):
		return ours, false
	default:
		return ours, true
	}
}

// mergeSet is the three-way set union: kept if both sides have it, or one
// side added it; removed if either side removed it. Order: theirs, then
// our additions. Elements added by ours are mapped through renames.
func mergeSet(base, ours, theirs []string, renames map[string]string) []string {
	if base == nil && ours == nil && theirs == nil {
		return nil
	}

	var out []string

	seen := map[string]bool{}
	add := func(v string) {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}

	for _, v := range theirs {
		if slices.Contains(ours, v) || !slices.Contains(base, v) { // both keep it, or theirs added it
			add(v)
		}
	}

	for _, v := range ours {
		if !slices.Contains(base, v) && !slices.Contains(theirs, v) { // ours added it
			if r, ok := renames[v]; ok {
				v = r
			}

			add(v)
		}
	}

	return out
}

// add3 merges an additive counter: ours + theirs - base.
func add3[N int | int64 | float64](base, ours, theirs N) N {
	return ours + theirs - base
}
