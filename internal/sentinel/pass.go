package sentinel

import (
	"context"
	"fmt"
	"reflect"

	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
)

// pass reads what the status depends on and records it. A switched-off
// Sentinel reads nothing beyond its settings.
func (c *Controller) pass(ctx context.Context) error {
	set, _, _, err := c.cfg.Store.SentinelSettings(ctx)
	if err != nil {
		return fmt.Errorf("read sentinel settings: %w", err)
	}

	now := c.cfg.Clock.Now()
	in := statusInput{Settings: set, LastPass: now, Now: now, Tick: c.cfg.Tick}

	if set.Enabled {
		windows, err := c.cfg.Store.SubscriptionWindows(ctx)
		if err != nil {
			return fmt.Errorf("read subscription windows: %w", err)
		}

		in.Mode = c.cfg.Channel.Effective(config.BackendNameAgent)
		in.Windows = windows
		// Sentinel dispatches no paid work, so nothing counts against the cap.
		in.GateOpen, in.GateReason = budgetGate(in.Mode, set, 0, windows, now)
	}

	c.record(in)

	return nil
}

// record keeps in as the last pass and publishes sentinel.changed when its
// status differs from the one last published.
func (c *Controller) record(in statusInput) {
	st := summarize(in)

	c.mu.Lock()
	c.last = in
	changed := !reflect.DeepEqual(st, c.published)
	c.published = st
	c.mu.Unlock()

	if changed {
		c.cfg.Bus.Publish(events.Event{Type: events.SentinelChanged, Timestamp: in.LastPass.UTC()})
	}
}
