package sentinel

import (
	"errors"
	"fmt"
	"math"
)

// ErrInvalidSettings wraps every Settings validation failure.
var ErrInvalidSettings = errors.New("invalid sentinel settings")

// Settings are the switches, caps and threshold an admin controls.
type Settings struct {
	Enabled               bool    `json:"enabled"`
	Healing               bool    `json:"healing"`
	Friction              bool    `json:"friction"`
	DailyCapUSD           float64 `json:"daily_cap_usd"`
	InvestigateCeilingUSD float64 `json:"investigate_ceiling_usd"`
	Headroom              float64 `json:"headroom"` // utilization fraction
}

// DefaultSettings is Sentinel off, with healing and friction on once enabled.
func DefaultSettings() Settings {
	return Settings{Healing: true, Friction: true, DailyCapUSD: 20, InvestigateCeilingUSD: 2, Headroom: 0.85}
}

// Validate rejects non-finite numbers and values outside their ranges.
func (s Settings) Validate() error {
	for _, n := range []float64{s.DailyCapUSD, s.InvestigateCeilingUSD, s.Headroom} {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return fmt.Errorf("%w: numbers must be finite", ErrInvalidSettings)
		}
	}

	if s.DailyCapUSD < 0 || s.DailyCapUSD > 10000 {
		return fmt.Errorf("%w: daily cap must be between 0 and 10000", ErrInvalidSettings)
	}

	if s.InvestigateCeilingUSD <= 0 || s.InvestigateCeilingUSD > 100 {
		return fmt.Errorf("%w: investigate ceiling must be above 0 and at most 100", ErrInvalidSettings)
	}

	if s.Headroom <= 0 || s.Headroom > 1 {
		return fmt.Errorf("%w: headroom must be above 0 and at most 1", ErrInvalidSettings)
	}

	return nil
}
