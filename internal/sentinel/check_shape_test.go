package sentinel

import (
	"testing"

	"github.com/stretchr/testify/assert"

	protocol "github.com/mhersson/contextmatrix-protocol"
)

func TestCheckShape(t *testing.T) {
	tests := []struct {
		name string
		fix  protocol.CardFix
		sent []string
		bad  bool
	}{
		{"empty fix", protocol.CardFix{}, nil, true},
		{"section only", protocol.CardFix{Section: "x"}, nil, false},
		{"verify only", protocol.CardFix{Verify: &protocol.VerifyConfig{Command: "make"}}, nil, false},
		{"retire a sent subtask", protocol.CardFix{RetireSubtasks: []string{"P-2"}}, []string{"P-2"}, false},
		{"retire an unsent subtask", protocol.CardFix{RetireSubtasks: []string{"P-3"}}, []string{"P-2"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkShape(fixContext{Fix: tt.fix, SentSubtasks: tt.sent})
			assert.Equal(t, tt.bad, len(got) > 0, "%v", got)
		})
	}
}
