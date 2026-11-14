package sentinel

import (
	"slices"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// trimFix decides whether a fix may apply, and in what form. A fix with no
// violations applies as it is. One whose violations all come from the section
// checks, that changes verify and retires nothing, applies without its
// section, and dropped names why; any other fix is refused.
func trimFix(fix protocol.CardFix, vs []violation) (kept protocol.CardFix, dropped []string, ok bool) {
	switch {
	case len(vs) == 0:
		return fix, nil, true
	case fix.Verify == nil, len(fix.RetireSubtasks) > 0, slices.ContainsFunc(vs, func(v violation) bool { return !v.section }):
		return fix, nil, false
	}

	fix.Section = ""

	return fix, texts(vs), true
}

// withFix is h holding fix in place of its investigation's, with the reasons
// its section was dropped, so every later read of the heal's fix gets it.
func withFix(h state.Heal, fix protocol.CardFix, dropped []string) state.Heal {
	inv := *h.Investigation
	inv.Fix = &fix
	h.Investigation, h.Dropped = &inv, dropped

	return h
}
