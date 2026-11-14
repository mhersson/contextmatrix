package sentinel

import (
	"encoding/json"
	"fmt"
	"math"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
)

// maxRunKindBody is the agent's cap on a run-kind request body.
const maxRunKindBody = 1 << 20

// cardFile is a card as its stored file.
type cardFile struct {
	ID, File string
}

// investigatePayload adds the card, then subtasks, then siblings, in order, while
// the encoded payload stays within 1 MiB, and returns the IDs it sent; false
// when the card alone does not fit. The first file that does not fit ends both
// lists, so what is sent is always a prefix.
func investigatePayload(
	base protocol.RunKindPayload, in protocol.InvestigateInput, subtasks, siblings []cardFile,
) (protocol.RunKindPayload, []string, []string, bool) {
	in.Subtasks, in.Siblings = nil, nil
	base.Kind, base.Check, base.Investigate = protocol.RunKindInvestigate, nil, &in

	size := jsonLen(base)
	if size > maxRunKindBody {
		return protocol.RunKindPayload{}, nil, nil, false
	}

	sentSubtasks, size, all := fill(&base, &in.Subtasks, subtasks, size)

	var sentSiblings []string
	if all {
		sentSiblings, _, _ = fill(&base, &in.Siblings, siblings, size)
	}

	return base, sentSubtasks, sentSiblings, true
}

// fill appends files to list, a list inside p, while p's encoding of size
// bytes stays within maxRunKindBody. It returns the IDs added, the new size
// and whether every file fit.
func fill(p *protocol.RunKindPayload, list *[]string, files []cardFile, size int) ([]string, int, bool) {
	var ids []string

	for _, f := range files {
		*list = append(*list, f.File)

		next := size + len(",") + jsonLen(f.File)
		if len(*list) == 1 {
			next = jsonLen(*p) // the first file also brings the field name
		}

		if next > maxRunKindBody {
			*list = (*list)[:len(*list)-1]

			return ids, size, false
		}

		ids = append(ids, f.ID)
		size = next
	}

	return ids, size, true
}

// jsonLen is v's encoded length; a value that cannot be encoded never fits.
func jsonLen(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return math.MaxInt
	}

	return len(b)
}

// checkPayload is the check run for a fix: a trial of after, the resolved
// verify the fix leaves (nil when the fix keeps verify), with before's command
// as the one in force, and the fix's section for the text check.
func checkPayload(base protocol.RunKindPayload, before, after *board.VerifyConfig, section string) protocol.RunKindPayload {
	in := protocol.CheckInput{Verify: protocolVerify(after), Text: section}
	if before != nil {
		in.CurrentCommand = before.Command
	}

	base.Kind, base.Investigate, base.Check = protocol.RunKindCheck, nil, &in

	return base
}

func protocolVerify(v *board.VerifyConfig) *protocol.VerifyConfig {
	if v == nil {
		return nil
	}

	return &protocol.VerifyConfig{Command: v.Command, TimeoutSeconds: v.TimeoutSeconds, Env: v.Env}
}

// investigateStopCause is the stop cause an investigation is told: none when
// CM's own stall or lost-worker detection ended the run.
func investigateStopCause(cause string) string {
	if cause == CauseStalled || cause == CauseUnknown {
		return ""
	}

	return cause
}

// cardFiles renders each card as its stored file.
func cardFiles(cards []*board.Card) ([]cardFile, error) {
	out := make([]cardFile, 0, len(cards))

	for _, c := range cards {
		b, err := board.SerializeCard(c)
		if err != nil {
			return nil, fmt.Errorf("render card %s: %w", c.ID, err)
		}

		out = append(out, cardFile{ID: c.ID, File: string(b)})
	}

	return out, nil
}
