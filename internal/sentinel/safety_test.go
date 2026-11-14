package sentinel

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactCardIDs(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		prefixes []string
		want     string
	}{
		{"listed prefix only", "see ABC-12 and XYZ-3", []string{"ABC"}, "see a card and XYZ-3"},
		{"several prefixes", "ABC-1, XYZ-3.", []string{"ABC", "XYZ"}, "a card, a card."},
		{"trailing letter", "ABC-12x", []string{"ABC"}, "ABC-12x"},
		{"leading letter", "XABC-12", []string{"ABC"}, "XABC-12"},
		{"lower case", "abc-12", []string{"ABC"}, "abc-12"},
		{"no prefixes", "ABC-12", nil, "ABC-12"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, redactCardIDs(tt.text, tt.prefixes))
		})
	}
}

func TestHoldsSecret(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		secrets []string
		want    bool
	}{
		{"eight bytes found", "key is abcd1234 here", []string{"abcd1234"}, true},
		{"seven bytes ignored", "key is abcd123 here", []string{"abcd123"}, false},
		{"absent", "nothing here", []string{"abcd1234"}, false},
		{"empty list", "abcd1234", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, holdsSecret(tt.text, tt.secrets))
		})
	}
}
