package state

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestSettingsValidateAnalyseCeiling(t *testing.T) {
	def := DefaultSettings()
	assert.False(t, def.AnalyseEveryRun)
	assert.InDelta(t, 1, def.AnalyseCeilingUSD, 1e-9)

	for _, c := range []float64{0, -1, 100.01, math.NaN(), math.Inf(1)} {
		s := DefaultSettings()
		s.AnalyseCeilingUSD = c
		require.ErrorIs(t, s.Validate(), ErrInvalidSettings, "%v", c)
	}

	for _, c := range []float64{0.01, 100} {
		s := DefaultSettings()
		s.AnalyseCeilingUSD = c
		assert.NoError(t, s.Validate(), "%v", c)
	}
}

func TestSettingsAnalyses(t *testing.T) {
	tests := []struct {
		name                               string
		enabled, friction, analyseEveryRun bool
		want                               bool
	}{
		{"all off", false, false, false, false},
		{"enabled only", true, false, false, false},
		{"friction only", false, true, false, false},
		{"analyse every run only", false, false, true, false},
		{"enabled and friction", true, true, false, false},
		{"enabled and analyse every run", true, false, true, false},
		{"friction and analyse every run", false, true, true, false},
		{"all on", true, true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Settings{Enabled: tt.enabled, Friction: tt.friction, AnalyseEveryRun: tt.analyseEveryRun}
			assert.Equal(t, tt.want, s.Analyses())
		})
	}
}
