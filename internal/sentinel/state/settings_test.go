package state

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSettingsValidate(t *testing.T) {
	with := func(f func(*Settings)) Settings {
		s := DefaultSettings()
		f(&s)

		return s
	}

	tests := []struct {
		name string
		set  Settings
		ok   bool
	}{
		{"defaults", DefaultSettings(), true},
		{"cap zero", with(func(s *Settings) { s.DailyCapUSD = 0 }), true},
		{"cap max", with(func(s *Settings) { s.DailyCapUSD = 10000 }), true},
		{"cap negative", with(func(s *Settings) { s.DailyCapUSD = -1 }), false},
		{"cap NaN", with(func(s *Settings) { s.DailyCapUSD = math.NaN() }), false},
		{"cap too high", with(func(s *Settings) { s.DailyCapUSD = 10001 }), false},
		{"ceiling zero", with(func(s *Settings) { s.InvestigateCeilingUSD = 0 }), false},
		{"ceiling max", with(func(s *Settings) { s.InvestigateCeilingUSD = 100 }), true},
		{"ceiling too high", with(func(s *Settings) { s.InvestigateCeilingUSD = 101 }), false},
		{"ceiling inf", with(func(s *Settings) { s.InvestigateCeilingUSD = math.Inf(1) }), false},
		{"headroom zero", with(func(s *Settings) { s.Headroom = 0 }), false},
		{"headroom one", with(func(s *Settings) { s.Headroom = 1 }), true},
		{"headroom over", with(func(s *Settings) { s.Headroom = 1.01 }), false},
		{"headroom NaN", with(func(s *Settings) { s.Headroom = math.NaN() }), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.set.Validate()
			if tc.ok {
				assert.NoError(t, err)

				return
			}

			assert.ErrorIs(t, err, ErrInvalidSettings)
		})
	}
}
