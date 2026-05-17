package service

import (
	"context"
	"fmt"
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
type MetricSeries struct {
	ActiveAgents []int `json:"active_agents"`
	InFlight     []int `json:"in_flight"`
	Stalled      []int `json:"stalled"`
	Shipped      []int `json:"shipped"`
}

// MetricSeriesDays is the number of daily samples in each MetricSeries slice.
const MetricSeriesDays = 8

// DashboardData contains all data needed for the project dashboard view.
type DashboardData struct {
	StateCounts           map[string]int `json:"state_counts"`
	ActiveAgents          []ActiveAgent  `json:"active_agents"`
	TotalCostUSD          float64        `json:"total_cost_usd"`
	CardsCompletedToday   int            `json:"cards_completed_today"`
	CardsCompletedLast7d  int            `json:"cards_completed_last_7d"`
	CardsCompletedPrior7d int            `json:"cards_completed_prior_7d"`
	MetricSeries          MetricSeries   `json:"metric_series"`
	AgentCosts            []AgentCost    `json:"agent_costs"`
	CardCosts             []CardCost     `json:"card_costs"`
}

// GetDashboard computes aggregated dashboard data for a project.
func (s *CardService) GetDashboard(ctx context.Context, project string) (*DashboardData, error) {
	cards, err := s.store.ListCards(ctx, project, storage.CardFilter{})
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}

	now := s.clk.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	last7dStart := now.Add(-7 * 24 * time.Hour)
	prior7dStart := now.Add(-14 * 24 * time.Hour)

	data := &DashboardData{
		StateCounts:  make(map[string]int),
		ActiveAgents: make([]ActiveAgent, 0),
		AgentCosts:   make([]AgentCost, 0),
		CardCosts:    make([]CardCost, 0),
		MetricSeries: MetricSeries{
			ActiveAgents: make([]int, MetricSeriesDays),
			InFlight:     make([]int, MetricSeriesDays),
			Stalled:      make([]int, MetricSeriesDays),
			Shipped:      make([]int, MetricSeriesDays),
		},
	}

	// Day boundaries for the sparkline window. dayEnds[i] is the end-of-day
	// instant for sample i; i=0 is 7 days ago, i=MetricSeriesDays-1 is today.
	// Today's end is the upcoming midnight (so "now" counts as part of today).
	dayEnds := make([]time.Time, MetricSeriesDays)
	for i := 0; i < MetricSeriesDays; i++ {
		offset := time.Duration(MetricSeriesDays-1-i) * 24 * time.Hour
		base := todayStart.Add(-offset)
		dayEnds[i] = base.Add(24 * time.Hour)
	}
	dayStarts := make([]time.Time, MetricSeriesDays)
	for i := 0; i < MetricSeriesDays; i++ {
		dayStarts[i] = dayEnds[i].Add(-24 * time.Hour)
	}

	agentCostMap := make(map[string]*AgentCost)

	for _, card := range cards {
		data.StateCounts[card.State]++

		// Active agents: cards with an assigned agent not in terminal states.
		if card.AssignedAgent != "" && card.State != board.StateDone && card.State != board.StateStalled && card.State != board.StateNotPlanned {
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

			data.ActiveAgents = append(data.ActiveAgents, aa)
		}

		// Cards completed today and in rolling 7d windows.
		if card.State == board.StateDone {
			if !card.Updated.Before(todayStart) {
				data.CardsCompletedToday++
			}
			if !card.Updated.Before(last7dStart) {
				data.CardsCompletedLast7d++
			} else if !card.Updated.Before(prior7dStart) {
				data.CardsCompletedPrior7d++
			}

			// Shipped sparkline: bucket each done card by the day it
			// transitioned to done (approximated by Updated). Accurate
			// because the Updated stamp on a done card is the moment
			// it landed in done.
			for i := 0; i < MetricSeriesDays; i++ {
				if !card.Updated.Before(dayStarts[i]) && card.Updated.Before(dayEnds[i]) {
					data.MetricSeries.Shipped[i]++
					break
				}
			}
		}

		// Reconstruct historical state at end-of-day for each sample. Walks
		// the card's state_changed activity-log entries; falls back to the
		// current state for cards that pre-date state-change logging.
		for i := 0; i < MetricSeriesDays; i++ {
			if card.Created.After(dayEnds[i]) {
				continue
			}

			state := stateAtTime(card, dayEnds[i])

			switch state {
			case board.StateInProgress, board.StateReview:
				data.MetricSeries.InFlight[i]++
				if card.AssignedAgent != "" {
					data.MetricSeries.ActiveAgents[i]++
				}
			case board.StateStalled:
				data.MetricSeries.Stalled[i]++
			}
		}

		// Cost aggregation.
		if card.TokenUsage != nil {
			data.TotalCostUSD += card.TokenUsage.EstimatedCostUSD

			data.CardCosts = append(data.CardCosts, CardCost{
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
		}
	}

	for _, ac := range agentCostMap {
		data.AgentCosts = append(data.AgentCosts, *ac)
	}

	return data, nil
}

// stateAtTime reconstructs a card's state at instant t by walking its
// state_changed activity log.
//
//	1. Latest entry whose Timestamp <= t exists  → use that entry's new-state.
//	2. All known entries have Timestamp > t      → use the oldest entry's
//	                                               from-state (the state the
//	                                               card sat in before any
//	                                               recorded transition).
//	3. No state_changed entries at all           → fall back to card.State
//	                                               (legacy data before the
//	                                               state-change log existed).
func stateAtTime(card *board.Card, t time.Time) string {
	var (
		latestTS    time.Time
		latestState string
		found       bool

		oldestTS   time.Time
		oldestFrom string
		anyEntry   bool
	)

	for _, e := range card.ActivityLog {
		if e.Action != stateChangedAction {
			continue
		}

		parts := strings.SplitN(e.Message, " -> ", 2)
		if len(parts) != 2 {
			continue
		}

		if !anyEntry || e.Timestamp.Before(oldestTS) {
			oldestTS = e.Timestamp
			oldestFrom = parts[0]
			anyEntry = true
		}

		if e.Timestamp.After(t) {
			continue
		}

		if !found || e.Timestamp.After(latestTS) {
			latestTS = e.Timestamp
			latestState = parts[1]
			found = true
		}
	}

	switch {
	case found:
		return latestState
	case anyEntry:
		return oldestFrom
	default:
		return card.State
	}
}
