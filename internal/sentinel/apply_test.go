package sentinel

import (
	"context"
	"errors"
	"testing"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

func TestFreshRerun(t *testing.T) {
	tests := []struct {
		name  string
		fix   *protocol.CardFix
		fresh bool
	}{
		{"a section", &protocol.CardFix{Section: "Run make generate first."}, true},
		{"retired subtasks", &protocol.CardFix{RetireSubtasks: []string{"ALPHA-011"}}, true},
		{"verify only", &protocol.CardFix{Verify: &protocol.VerifyConfig{Command: "make check"}}, false},
		{"no fix", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.fresh, freshRerun(tc.fix))
		})
	}
}

func sectionFix() *protocol.CardFix {
	return &protocol.CardFix{
		Section: "Run make generate first.",
		Verify:  &protocol.VerifyConfig{Command: "make check", TimeoutSeconds: 900},
	}
}

// applyEnv stores ALPHA-001 stopped mid-execute with unstarted subtask
// ALPHA-011, and a heal at apply holding fix under a diagnosis of cause.
func applyEnv(t *testing.T, cause string, fix *protocol.CardFix) (*dispatchEnv, *Heal, *board.Card, Run) {
	t.Helper()

	e := newPassEnv(t)
	card, run := e.stopped("ALPHA-001", protocol.StopCauseVerifyParked, func(c *board.Card, _ *Run) {
		c.Phase = "execute"
	})
	e.cards.cards = append(e.cards.cards, &board.Card{
		ID: "ALPHA-011", Project: "alpha", Title: "Part two", Parent: "ALPHA-001", State: board.StateTodo,
		Updated: epoch.Add(-30 * time.Minute),
	})
	h := e.openHeal(t, "ALPHA-001", StageApply, investigated(cause, fix))

	return e, &h, card, run
}

func storedCard(t *testing.T, e *dispatchEnv, id string) *board.Card {
	t.Helper()

	c, err := e.cards.GetCard(context.Background(), "alpha", id)
	require.NoError(t, err)

	return c
}

func TestApplyFixOneGuardedWrite(t *testing.T) {
	e, h, card, _ := applyEnv(t, protocol.DiagnosisCard, sectionFix())

	change, err := e.ctl.applyFix(context.Background(), h, card)
	require.NoError(t, err)

	require.Len(t, e.cards.patches, 1)
	in := e.cards.patches[0].In
	require.NotNil(t, in.ExpectUpdated)
	assert.Equal(t, card.Updated, *in.ExpectUpdated)
	assert.Equal(t, Agent, in.AgentID)
	assert.Equal(t, &service.SectionPatch{Heading: SectionHeading, Content: "Run make generate first."}, in.UpsertSection)
	assert.Equal(t, &board.VerifyConfig{Command: "make check", TimeoutSeconds: 900}, in.Verify)
	require.NotNil(t, in.Phase)
	assert.Empty(t, *in.Phase, "a fresh rerun starts from planning")

	written := storedCard(t, e, "ALPHA-001")
	assert.Empty(t, written.Phase)
	assert.True(t, change.Fresh)
	assert.Empty(t, change.SectionBefore)
	assert.Contains(t, change.SectionAfter, "Run make generate first.")
	assert.Nil(t, change.VerifyBefore)
	assert.Equal(t, written.Verify, change.VerifyAfter)
}

func TestApplyFixVerifyOnlyKeepsPhase(t *testing.T) {
	fix := &protocol.CardFix{Verify: &protocol.VerifyConfig{Command: "make check"}}
	e, h, card, _ := applyEnv(t, protocol.DiagnosisCard, fix)

	change, err := e.ctl.applyFix(context.Background(), h, card)
	require.NoError(t, err)

	require.Len(t, e.cards.patches, 1)
	assert.Nil(t, e.cards.patches[0].In.Phase)
	assert.Nil(t, e.cards.patches[0].In.UpsertSection)
	assert.Equal(t, "execute", storedCard(t, e, "ALPHA-001").Phase, "a verify-only rerun resumes")
	assert.False(t, change.Fresh)
}

func TestApplyFixModelDiagnosisWritesNothing(t *testing.T) {
	// A model diagnosis holds no fix, whatever its fix field carries.
	e, h, card, _ := applyEnv(t, protocol.DiagnosisModel, sectionFix())

	change, err := e.ctl.applyFix(context.Background(), h, card)
	require.NoError(t, err)

	assert.Empty(t, e.cards.patches)
	assert.Zero(t, e.store.refreshes)
	assert.Equal(t, Change{}, change)
}

func TestApplyFixHumanEditInBetween(t *testing.T) {
	fix := sectionFix()
	fix.RetireSubtasks = []string{"ALPHA-011"}
	e, h, card, _ := applyEnv(t, protocol.DiagnosisCard, fix)

	// A human saves the card after the pass read it.
	edited := *card
	edited.Updated = card.Updated.Add(time.Minute)
	e.cards.cards[0] = &edited

	_, err := e.ctl.applyFix(context.Background(), h, card)
	require.ErrorIs(t, err, service.ErrCardChanged)

	assert.Len(t, e.cards.patches, 1, "no subtask is retired")
	assert.Equal(t, board.StateTodo, storedCard(t, e, "ALPHA-011").State)
	assert.Zero(t, e.store.refreshes)
}

func TestApplyFixRetiresAsSentinel(t *testing.T) {
	fix := sectionFix()
	fix.RetireSubtasks = []string{"ALPHA-011"}
	e, h, card, _ := applyEnv(t, protocol.DiagnosisCard, fix)
	sub := storedCard(t, e, "ALPHA-011")

	change, err := e.ctl.applyFix(context.Background(), h, card)
	require.NoError(t, err)

	require.Len(t, e.cards.patches, 2)
	retire := e.cards.patches[1]
	assert.Equal(t, "ALPHA-011", retire.ID)
	require.NotNil(t, retire.In.State)
	assert.Equal(t, board.StateNotPlanned, *retire.In.State)
	assert.Equal(t, Agent, retire.In.AgentID)
	require.NotNil(t, retire.In.ExpectUpdated)
	assert.Equal(t, sub.Updated, *retire.In.ExpectUpdated)

	assert.Equal(t, board.StateNotPlanned, storedCard(t, e, "ALPHA-011").State)
	assert.Equal(t, []string{"ALPHA-011"}, change.Retired)
	assert.True(t, change.Fresh)
}

func TestApplyFixRefreshesSnapshot(t *testing.T) {
	e, h, card, _ := applyEnv(t, protocol.DiagnosisCard, sectionFix())

	_, err := e.ctl.applyFix(context.Background(), h, card)
	require.NoError(t, err)

	written := storedCard(t, e, "ALPHA-001")
	snap := e.store.run("alpha", "ALPHA-001").Launch.Snapshot
	assert.Equal(t, board.TakeEditSnapshot(written), snap)
	assert.Empty(t, checkEdits(fixContext{Card: written, Snapshot: snap}), "Sentinel's own write is no edit")
}

func TestApplyParksARefusedRetire(t *testing.T) {
	fix := sectionFix()
	fix.RetireSubtasks = []string{"ALPHA-011"}
	e, h, card, run := applyEnv(t, protocol.DiagnosisCard, fix)
	e.cards.refuse = map[string]error{"ALPHA-011": errors.New("validate transition: todo to not_planned is not allowed")}

	require.NoError(t, e.ctl.apply(context.Background(), h, card, run))

	stored := e.store.heal(h.ID)
	assert.Equal(t, StageParked, stored.Stage)
	assert.Contains(t, stored.Reason, "ALPHA-011")
	assert.Contains(t, stored.Reason, "todo to not_planned is not allowed")
	assert.Empty(t, e.relaunched)
	assert.Zero(t, e.store.run("alpha", "ALPHA-001").Attempts)
}
