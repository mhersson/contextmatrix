package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// ActiveAgent describes an agent currently working on a card.
type ActiveAgent struct {
	AgentID       string    `json:"agent_id"`
	CardID        string    `json:"card_id"`
	CardTitle     string    `json:"card_title"`
	Since         time.Time `json:"since"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// AgentCost contains per-agent cost aggregation.
type AgentCost struct {
	AgentID          string  `json:"agent_id"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	CardCount        int     `json:"card_count"`
}

// ModelCost contains per-model cost aggregation. Cards whose TokenUsage has
// no Model set are bucketed under "unknown" so totals reconcile.
type ModelCost struct {
	Model            string  `json:"model"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	CardCount        int     `json:"card_count"`
}

// CardCost contains per-card cost summary.
type CardCost struct {
	CardID           string  `json:"card_id"`
	CardTitle        string  `json:"card_title"`
	AssignedAgent    string  `json:"assigned_agent,omitempty"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

// MetricSeries holds an 8-sample daily window (oldest first, today last) for
// each tile on the board's metrics ribbon. Shipped is bucketed by Updated
// across cards in the done state. The other three are reconstructed by
// walking each card's state_changed activity-log entries — accurate going
// forward from when state-change logging was introduced; for older cards
// without state-change entries the sparkline falls back to the card's
// current state. ActiveAgents counts cards where the reconstructed state
// is in_progress/review and the card currently has an assigned agent
// (claim history isn't tracked, so per-day agent presence is approximate).
// The *Parents variants (InFlightParents, StalledParents, ShippedParents)
// mirror the above but exclude subtasks (cards with a non-empty Parent field).
// ActiveAgents has no parents variant by design — an agent working a subtask
// is still real activity.
type MetricSeries struct {
	ActiveAgents    []int `json:"active_agents"`
	InFlight        []int `json:"in_flight"`
	Stalled         []int `json:"stalled"`
	Shipped         []int `json:"shipped"`
	InFlightParents []int `json:"in_flight_parents"`
	StalledParents  []int `json:"stalled_parents"`
	ShippedParents  []int `json:"shipped_parents"`
}

// MetricSeriesDays is the number of daily samples in each MetricSeries slice.
const MetricSeriesDays = 8

// DashboardData contains all data needed for the project dashboard view.
type DashboardData struct {
	StateCounts                  map[string]int `json:"state_counts"`
	StateCountsParents           map[string]int `json:"state_counts_parents"`
	ActiveAgents                 []ActiveAgent  `json:"active_agents"`
	TotalCostUSD                 float64        `json:"total_cost_usd"`
	TotalCostUSDLast30d          float64        `json:"total_cost_usd_last_30d"`
	TotalCostUSDPrior30d         float64        `json:"total_cost_usd_prior_30d"`
	CostSeries30d                []float64      `json:"cost_series_30d"`
	CardsCompletedToday          int            `json:"cards_completed_today"`
	CardsCompletedTodayParents   int            `json:"cards_completed_today_parents"`
	CardsCompletedLast7d         int            `json:"cards_completed_last_7d"`
	CardsCompletedLast7dParents  int            `json:"cards_completed_last_7d_parents"`
	CardsCompletedPrior7d        int            `json:"cards_completed_prior_7d"`
	CardsCompletedPrior7dParents int            `json:"cards_completed_prior_7d_parents"`
	MetricSeries                 MetricSeries   `json:"metric_series"`
	AgentCosts                   []AgentCost    `json:"agent_costs"`
	ModelCosts                   []ModelCost    `json:"model_costs"`
	CardCosts                    []CardCost     `json:"card_costs"`
}

// GetDashboard computes aggregated dashboard data for a project.
func (s *CardService) GetDashboard(ctx context.Context, project string) (*DashboardData, error) {
	cards, err := s.store.ListCards(ctx, project, storage.CardFilter{})
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}

	now := s.clk.Now()
	tz := now.Location()

	// State counts: too trivial to extract — just two lines per card.
	stateCounts := make(map[string]int)
	stateCountsParents := make(map[string]int)

	for _, card := range cards {
		stateCounts[card.State]++
		if card.Parent == "" {
			stateCountsParents[card.State]++
		}
	}

	agentCosts, modelCosts, cardCosts, totalCostUSD := aggregateCostsByAgentModel(cards)
	completions := bucketCompletions(cards, now, tz)
	sparkline := bucketSparkline(cards, now, tz)
	activeAgents := buildAgentList(cards, now)
	costLast30d, costPrior30d, costSeries30d := bucketCostSeries(cards, now, tz)

	return &DashboardData{
		StateCounts:                  stateCounts,
		StateCountsParents:           stateCountsParents,
		ActiveAgents:                 activeAgents,
		TotalCostUSD:                 totalCostUSD,
		TotalCostUSDLast30d:          costLast30d,
		TotalCostUSDPrior30d:         costPrior30d,
		CostSeries30d:                costSeries30d,
		CardsCompletedToday:          completions.today,
		CardsCompletedTodayParents:   completions.todayParents,
		CardsCompletedLast7d:         completions.last7d,
		CardsCompletedLast7dParents:  completions.last7dParents,
		CardsCompletedPrior7d:        completions.prior7d,
		CardsCompletedPrior7dParents: completions.prior7dParents,
		MetricSeries:                 sparkline,
		AgentCosts:                   agentCosts,
		ModelCosts:                   modelCosts,
		CardCosts:                    cardCosts,
	}, nil
}

// completionCounts holds the rolling-window completion counters returned by
// bucketCompletions.
type completionCounts struct {
	today          int
	todayParents   int
	last7d         int
	last7dParents  int
	prior7d        int
	prior7dParents int
}

// buildAgentList returns the slice of ActiveAgent entries for cards that
// currently have an assigned agent in a non-terminal state.
func buildAgentList(cards []*board.Card, now time.Time) []ActiveAgent {
	_ = now // reserved for future relative-since calculations
	out := make([]ActiveAgent, 0)

	for _, card := range cards {
		if card.AssignedAgent == "" {
			continue
		}

		if card.State == board.StateDone || card.State == board.StateStalled || card.State == board.StateNotPlanned {
			continue
		}

		aa := ActiveAgent{
			AgentID:   card.AssignedAgent,
			CardID:    card.ID,
			CardTitle: card.Title,
			Since:     card.Updated,
		}
		if card.LastHeartbeat != nil {
			aa.LastHeartbeat = *card.LastHeartbeat
			aa.Since = *card.LastHeartbeat
		}

		out = append(out, aa)
	}

	return out
}

// bucketCompletions counts done-cards falling into today / last-7d / prior-7d
// windows, splitting parent-only cards into the *Parents variants.
func bucketCompletions(cards []*board.Card, now time.Time, tz *time.Location) completionCounts {
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, tz)
	last7dStart := now.Add(-7 * 24 * time.Hour)
	prior7dStart := now.Add(-14 * 24 * time.Hour)

	var counts completionCounts

	for _, card := range cards {
		if card.State != board.StateDone {
			continue
		}

		isParent := card.Parent == ""

		if !card.Updated.Before(todayStart) {
			counts.today++
			if isParent {
				counts.todayParents++
			}
		}

		if !card.Updated.Before(last7dStart) {
			counts.last7d++
			if isParent {
				counts.last7dParents++
			}
		} else if !card.Updated.Before(prior7dStart) {
			counts.prior7d++
			if isParent {
				counts.prior7dParents++
			}
		}
	}

	return counts
}

// bucketSparkline builds the 8-sample MetricSeries for the dashboard ribbon.
// Shipped is bucketed by Updated (accurate for done cards). InFlight, Stalled,
// and ActiveAgents are reconstructed by replaying each card's state_changed
// activity-log entries.
func bucketSparkline(cards []*board.Card, now time.Time, tz *time.Location) MetricSeries {
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, tz)

	// Day boundaries for the sparkline window. dayEnds[i] is the end-of-day
	// instant for sample i; i=0 is 7 days ago, i=MetricSeriesDays-1 is today.
	// Today's end is the upcoming midnight (so "now" counts as part of today).
	dayStarts := make([]time.Time, MetricSeriesDays)
	dayEnds := make([]time.Time, MetricSeriesDays)

	for i := range MetricSeriesDays {
		offset := time.Duration(MetricSeriesDays-1-i) * 24 * time.Hour
		dayStarts[i] = todayStart.Add(-offset)
		dayEnds[i] = dayStarts[i].Add(24 * time.Hour)
	}

	ms := MetricSeries{
		ActiveAgents:    make([]int, MetricSeriesDays),
		InFlight:        make([]int, MetricSeriesDays),
		Stalled:         make([]int, MetricSeriesDays),
		Shipped:         make([]int, MetricSeriesDays),
		InFlightParents: make([]int, MetricSeriesDays),
		StalledParents:  make([]int, MetricSeriesDays),
		ShippedParents:  make([]int, MetricSeriesDays),
	}

	for _, card := range cards {
		isParent := card.Parent == ""

		// Shipped sparkline: bucket each done card by the day it
		// transitioned to done (approximated by Updated). Accurate
		// because the Updated stamp on a done card is the moment
		// it landed in done.
		if card.State == board.StateDone {
			for i := range MetricSeriesDays {
				if !card.Updated.Before(dayStarts[i]) && card.Updated.Before(dayEnds[i]) {
					ms.Shipped[i]++
					if isParent {
						ms.ShippedParents[i]++
					}

					break
				}
			}
		}

		// Reconstruct historical state at end-of-day for each sample.
		// Extract the card's state_changed entries once, then sweep the
		// 8 day-end instants against the sorted slice in O(N+8) rather
		// than O(N * 8) repeated full walks per card.
		changes, baseline := extractStateChanges(card)

		for i := range MetricSeriesDays {
			if card.Created.After(dayEnds[i]) {
				continue
			}

			state := stateAtTimeFromChanges(card, changes, baseline, dayEnds[i])

			switch state {
			case board.StateInProgress, board.StateReview:
				ms.InFlight[i]++
				if isParent {
					ms.InFlightParents[i]++
				}

				if card.AssignedAgent != "" {
					ms.ActiveAgents[i]++
				}
			case board.StateStalled:
				ms.Stalled[i]++
				if isParent {
					ms.StalledParents[i]++
				}
			}
		}
	}

	return ms
}

// aggregateCostsByAgentModel rolls up token usage and estimated cost per agent
// and per model. Returns sorted slices (cost desc, name asc on ties) ready for
// the wire, the per-card cost list, and the grand total.
//
// Map iteration is randomized, so the sort is a determinism guarantee at the
// API boundary — the frontend re-sorts for display.
func aggregateCostsByAgentModel(cards []*board.Card) (agentCosts []AgentCost, modelCosts []ModelCost, cardCosts []CardCost, totalCostUSD float64) {
	agentCostMap := make(map[string]*AgentCost)
	modelCostMap := make(map[string]*ModelCost)
	cardCosts = make([]CardCost, 0)

	for _, card := range cards {
		if card.TokenUsage == nil {
			continue
		}

		totalCostUSD += card.TokenUsage.EstimatedCostUSD

		cardCosts = append(cardCosts, CardCost{
			CardID:           card.ID,
			CardTitle:        card.Title,
			AssignedAgent:    card.AssignedAgent,
			PromptTokens:     card.TokenUsage.PromptTokens,
			CompletionTokens: card.TokenUsage.CompletionTokens,
			EstimatedCostUSD: card.TokenUsage.EstimatedCostUSD,
		})

		agent := card.AssignedAgent
		if agent == "" {
			agent = "unassigned"
		}

		ac, ok := agentCostMap[agent]
		if !ok {
			ac = &AgentCost{AgentID: agent}
			agentCostMap[agent] = ac
		}

		ac.PromptTokens += card.TokenUsage.PromptTokens
		ac.CompletionTokens += card.TokenUsage.CompletionTokens
		ac.EstimatedCostUSD += card.TokenUsage.EstimatedCostUSD
		ac.CardCount++

		// Skip cards with no measurable usage. Zero-token, zero-cost
		// entries (e.g. cards that recorded a TokenUsage struct but
		// never accumulated anything) would otherwise inflate the
		// "unknown" bucket's card_count without contributing real
		// cost. The agent bucket above keeps them because agent
		// attribution is meaningful even at zero, but the model
		// rollup is purely a cost view.
		if card.TokenUsage.PromptTokens == 0 && card.TokenUsage.CompletionTokens == 0 && card.TokenUsage.EstimatedCostUSD == 0 {
			continue
		}

		model := card.TokenUsage.Model
		if model == "" {
			model = "unknown"
		}

		mc, ok := modelCostMap[model]
		if !ok {
			mc = &ModelCost{Model: model}
			modelCostMap[model] = mc
		}

		mc.PromptTokens += card.TokenUsage.PromptTokens
		mc.CompletionTokens += card.TokenUsage.CompletionTokens
		mc.EstimatedCostUSD += card.TokenUsage.EstimatedCostUSD
		mc.CardCount++
	}

	agentCosts = make([]AgentCost, 0, len(agentCostMap))
	for _, ac := range agentCostMap {
		agentCosts = append(agentCosts, *ac)
	}

	modelCosts = make([]ModelCost, 0, len(modelCostMap))
	for _, mc := range modelCostMap {
		modelCosts = append(modelCosts, *mc)
	}

	// Stable wire ordering: cost desc, identifier asc on ties.
	sort.Slice(agentCosts, func(i, j int) bool {
		if agentCosts[i].EstimatedCostUSD != agentCosts[j].EstimatedCostUSD {
			return agentCosts[i].EstimatedCostUSD > agentCosts[j].EstimatedCostUSD
		}

		return agentCosts[i].AgentID < agentCosts[j].AgentID
	})

	sort.Slice(modelCosts, func(i, j int) bool {
		if modelCosts[i].EstimatedCostUSD != modelCosts[j].EstimatedCostUSD {
			return modelCosts[i].EstimatedCostUSD > modelCosts[j].EstimatedCostUSD
		}

		return modelCosts[i].Model < modelCosts[j].Model
	})

	return agentCosts, modelCosts, cardCosts, totalCostUSD
}

// bucketCostSeries computes cost aggregates over a 30-day sliding window.
// It returns:
//   - last30d: sum of EstimatedCostUSD for cards whose Updated is within the
//     last 30 days (>= dayStarts[0]).
//   - prior30d: sum for cards whose Updated falls in the prior 30-day window
//     (dayStarts[0]-30*24h <= Updated < dayStarts[0]).
//   - series30d: 30-element daily bucket slice (index 0 = oldest day, 29 = today).
//
// Cards with nil TokenUsage are skipped entirely. Cards older than 60 days from
// dayStarts[0] are excluded from all three accumulators.
func bucketCostSeries(cards []*board.Card, now time.Time, tz *time.Location) (last30d, prior30d float64, series30d []float64) {
	const numDays = 30

	series30d = make([]float64, numDays)

	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, tz)

	// dayStarts[i] = todayStart - (29-i)*24h  → index 0 is the oldest bucket.
	dayStarts := make([]time.Time, numDays)
	dayEnds := make([]time.Time, numDays)

	for i := range numDays {
		offset := time.Duration(numDays-1-i) * 24 * time.Hour
		dayStarts[i] = todayStart.Add(-offset)
		dayEnds[i] = dayStarts[i].Add(24 * time.Hour)
	}

	// Window boundaries.
	// "Last 30 days" = the 30 daily buckets ending at the next tz midnight
	// (so the actual window spans 30 * 24h aligned on local midnight, not
	// strictly now-720h).
	windowStart := dayStarts[0]                         // start of the 30-day window
	priorStart := windowStart.Add(-30 * 24 * time.Hour) // start of the prior 30d window

	for _, card := range cards {
		if card.TokenUsage == nil {
			continue
		}

		updated := card.Updated
		cost := card.TokenUsage.EstimatedCostUSD

		// Exclude cards older than 60 days (i.e. before priorStart).
		if updated.Before(priorStart) {
			continue
		}

		if !updated.Before(windowStart) {
			// Within the last 30 days.
			last30d += cost

			// Find the matching day bucket via linear scan.
			for i := range numDays {
				if !updated.Before(dayStarts[i]) && updated.Before(dayEnds[i]) {
					series30d[i] += cost

					break
				}
			}
		} else {
			// Prior 30-day window: priorStart <= updated < windowStart.
			prior30d += cost
		}
	}

	return last30d, prior30d, series30d
}

// ActivityFeedEntry is one row in the cross-card activity feed. Mirrors a
// board.ActivityEntry with the owning card's ID stamped on so a flattened
// feed can route to source.
type ActivityFeedEntry struct {
	Agent   string    `json:"agent"`
	Action  string    `json:"action"`
	Message string    `json:"message,omitempty"`
	CardID  string    `json:"card_id"`
	TS      time.Time `json:"ts"`
}

// ListActivity returns the `limit` most-recent activity-log entries across
// all cards in the project, newest first. Caps `limit` to 500 at the
// service boundary so handlers don't need to repeat the constant.
//
// Today this iterates the card cache, flattens each card's log, sorts, and
// truncates. For projects in the low-thousands of cards it is fine; if it
// ever becomes a hot path, the store can grow a dedicated index. Lives in
// the service layer (not the handler) so future consumers — MCP tool, CLI,
// alternate UI — reuse the same primitive.
func (s *CardService) ListActivity(ctx context.Context, project string, limit int) ([]ActivityFeedEntry, error) {
	if limit <= 0 {
		limit = 50
	}

	if limit > 500 {
		limit = 500
	}

	cards, err := s.store.ListCards(ctx, project, storage.CardFilter{})
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}

	totalEntries := 0
	for _, c := range cards {
		totalEntries += len(c.ActivityLog)
	}

	out := make([]ActivityFeedEntry, 0, totalEntries)

	for _, c := range cards {
		for _, e := range c.ActivityLog {
			out = append(out, ActivityFeedEntry{
				Agent:   e.Agent,
				Action:  e.Action,
				Message: e.Message,
				CardID:  c.ID,
				TS:      e.Timestamp,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].TS.After(out[j].TS) })

	if len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}

// stateChange is a parsed state_changed activity-log entry: a transition from
// `from` to `to` at instant `ts`. Used by the sparkline reconstruction to
// avoid re-parsing the message string on every day-end sample.
type stateChange struct {
	ts   time.Time
	from string
	to   string
}

// extractStateChanges parses a card's state_changed activity-log entries into
// a slice of stateChange, sorted ascending by ts. The returned `baseline` is
// the `from` state of the oldest entry (the state the card sat in before any
// recorded transition); empty when no state_changed entries exist. Cards that
// pre-date state-change logging have no entries and the dashboard falls back
// to card.State (legacy behavior, preserved).
func extractStateChanges(card *board.Card) ([]stateChange, string) {
	changes := make([]stateChange, 0, len(card.ActivityLog))

	for _, e := range card.ActivityLog {
		if e.Action != stateChangedAction {
			continue
		}

		parts := strings.SplitN(e.Message, " -> ", 2)
		if len(parts) != 2 {
			continue
		}

		changes = append(changes, stateChange{ts: e.Timestamp, from: parts[0], to: parts[1]})
	}

	if len(changes) == 0 {
		return nil, ""
	}

	// Stable sort preserves activity-log insertion order as the tiebreaker
	// when two state_changed entries share a timestamp — important because
	// stateAtTimeFromChanges treats the latest entry at-or-before t as
	// authoritative and we want that to be the latest by insertion order
	// when timestamps collide.
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].ts.Before(changes[j].ts) })

	return changes, changes[0].from
}

// stateAtTimeFromChanges returns the card's state at instant t given a
// pre-sorted (ascending by ts) slice of stateChange and the baseline state.
// Semantics mirror the original stateAtTime:
//
//  1. Latest change whose ts <= t exists  → use that change's `to`.
//  2. All known changes have ts > t       → use `baseline` (the `from`
//     of the oldest recorded transition).
//  3. No state_changed entries at all     → fall back to card.State
//     (legacy data before the state-change log existed).
//
// O(log N) via binary search on the sorted slice.
func stateAtTimeFromChanges(card *board.Card, changes []stateChange, baseline string, t time.Time) string {
	if len(changes) == 0 {
		return card.State
	}

	// Find the index of the first change whose ts > t; the change before
	// that index is the latest change at-or-before t.
	idx := sort.Search(len(changes), func(i int) bool {
		return changes[i].ts.After(t)
	})

	if idx == 0 {
		return baseline
	}

	return changes[idx-1].to
}
