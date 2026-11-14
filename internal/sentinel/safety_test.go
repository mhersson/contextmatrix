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
		{"an empty prefix is skipped", "x-5 and ABC-1", []string{"", "ABC"}, "x-5 and a card"},
		{"only an empty prefix", "x-5", []string{""}, "x-5"},
		{"a card branch", "push cm/abc-12 first", []string{"ABC"}, "push a card branch first"},
		{"a card branch with a key", "cm/abc-12-PROJ-7 is ahead", []string{"ABC"}, "a card branch is ahead"},
		{"a card branch with a word", "cm/abc-12-retry.", []string{"ABC"}, "a card branch."},
		{"a branch on a remote", "origin/cm/abc-12", []string{"ABC"}, "origin/a card branch"},
		{"a branch of an unlisted prefix", "cm/xyz-3", []string{"ABC"}, "cm/xyz-3"},
		{"a branch with a trailing letter", "cm/abc-12x", []string{"ABC"}, "cm/abc-12x"},
		{"an upper-case branch keeps the ID rule", "cm/ABC-12", []string{"ABC"}, "cm/a card"},
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
