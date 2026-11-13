package sentinel

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
)

// RunKindStatus records a validated run-kind terminal status on the card's
// heal and charges it once per run ID, whatever the heal's stage; it never
// applies the result. A repeat, or a status no heal waits on, changes nothing.
func (c *Controller) RunKindStatus(ctx context.Context, p protocol.StatusCallbackPayload) error {
	now := c.cfg.Clock.Now()

	report, err := runKindReport(p, c.cfg.Pricer, now)
	if err != nil {
		return err
	}

	healID, recorded, err := c.cfg.Store.RecordRunKindStatus(ctx, report)
	if err != nil {
		return fmt.Errorf("record run kind status: %w", err)
	}

	if !recorded {
		return nil
	}

	if windows := usageWindows(p.Usage); len(windows) > 0 {
		c.cfg.Channel.RecordUsage(ctx, windows)
	}

	// The status is recorded, so a retry would be a repeat: a failure from here
	// on is logged, not returned.
	c.storeFindings(ctx, healID, p, now)

	c.cfg.Bus.Publish(events.Event{Type: events.SentinelChanged, Project: p.Project, CardID: p.CardID, Timestamp: now.UTC()})
	c.Nudge()

	return nil
}

// runKindReport is the store's record of a run kind's terminal status. A model
// kind that reported no usage is charged its ceiling.
func runKindReport(p protocol.StatusCallbackPayload, pricer Pricer, at time.Time) (RunKindReport, error) {
	r := RunKindReport{
		Project: p.Project, CardID: p.CardID, Kind: p.Kind, RunID: p.RunID,
		SpendUSD:      priceUsage(p.Usage, pricer),
		ChargeCeiling: p.Kind == protocol.RunKindInvestigate && len(p.Usage) == 0,
		At:            at,
	}

	result := kindResult(p)
	if result == nil {
		r.Failure = cmp.Or(p.Message, p.StopCause, p.WorkerStatus)

		return r, nil
	}

	var err error
	if r.Result, err = json.Marshal(result); err != nil {
		return RunKindReport{}, fmt.Errorf("encode run kind result: %w", err)
	}

	return r, nil
}

// kindResult is the result member the status's kind names; nil when absent.
func kindResult(p protocol.StatusCallbackPayload) any {
	switch {
	case p.Result == nil:
		return nil
	case p.Kind == protocol.RunKindCheck && p.Result.Check != nil:
		return p.Result.Check
	case p.Kind == protocol.RunKindInvestigate && p.Result.Investigate != nil:
		return p.Result.Investigate
	}

	return nil
}

// storeFindings keeps an investigation's friction findings while the friction
// switch is on.
func (c *Controller) storeFindings(ctx context.Context, healID int64, p protocol.StatusCallbackPayload, at time.Time) {
	if p.Result == nil || p.Result.Investigate == nil || len(p.Result.Investigate.Findings) == 0 {
		return
	}

	log := ctxlog.Logger(ctx).With("project", p.Project, "card_id", p.CardID, "run_id", p.RunID)

	set, _, _, err := c.cfg.Store.SentinelSettings(ctx)
	if err != nil {
		log.Warn("sentinel: findings not stored", "error", err)

		return
	}

	if !set.Friction {
		return
	}

	if err := c.cfg.Store.AddFindings(ctx, healID, p.Project, p.CardID, p.Result.Investigate.Findings, at); err != nil {
		log.Warn("sentinel: findings not stored", "error", err)
	}
}
