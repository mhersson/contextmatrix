package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTerminal(t *testing.T) {
	for stage, want := range map[string]bool{
		StageInvestigate:   false,
		StageCheck:         false,
		StageApply:         false,
		StageUsageWait:     false,
		StageRerun:         false,
		StageForwarded:     false,
		StageHealed:        true,
		StageRerunFailed:   true,
		StageParked:        true,
		StageClosed:        true,
		StageForwardPassed: true,
		StageForwardFailed: true,

		StageRevise:          false,
		StageRevised:         true,
		StageRevisionStale:   true,
		StageRevisionRefused: true,
		StageRevisionFailed:  true,
	} {
		assert.Equal(t, want, Terminal(stage), stage)
	}
}
