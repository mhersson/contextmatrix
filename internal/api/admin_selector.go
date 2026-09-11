package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix-protocol/selection"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/modelcatalog"
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
)

// selectorSettingsReader is the read side of the stored selector settings:
// the per-role ladders and the price headroom. The trigger path needs only
// this; the admin endpoints need selectorAdminStore.
type selectorSettingsReader interface {
	SelectorLadders(ctx context.Context) (map[string]map[string]float64, time.Time, error)
	SelectorHeadroom(ctx context.Context) (float64, time.Time, error)
}

// selectorAdminStore is the op-store surface the admin selector endpoints
// need. opstore/sqlite.Store implements it.
type selectorAdminStore interface {
	selectorSettingsReader
	PutSelectorLadders(ctx context.Context, ladders map[string]map[string]float64) error
	PutSelectorHeadroom(ctx context.Context, headroom float64) error
}

// selectorCatalog is the catalog surface the candidates and preview
// endpoints need: the candidate set, the floor it was built with, and when
// it was built. Provenance names where each candidate's price came from
// (gateway, aa, token_costs, none) and which AA row scored it, so the page
// can mark a list price and name the row. Implemented by
// modelcatalog.Builder; wider than catalogProvider (trigger path) for the
// same narrow-interface reason as blacklistAdminStore.
type selectorCatalog interface {
	Candidates(ctx context.Context) []protocol.CandidateModel
	Provenance(ctx context.Context) map[string]modelcatalog.CandidateProvenance
	Floor() float64
	LastRefreshed(ctx context.Context) time.Time
}

// ErrCodeCatalogUnavailable -> 503: no candidate catalog is configured, or
// it has not completed its first refresh.
const ErrCodeCatalogUnavailable = "CATALOG_UNAVAILABLE"

// previewPanelSeats is the review panel size the preview shows: the
// three-seat panel the agent convenes for a review.
const previewPanelSeats = 3

// selectorAdminHandlers serves /api/admin/selector/*: the stored ladders,
// the candidate catalog behind them, and a preview of what the shared
// selector would pick under a ladder the operator is editing.
type selectorAdminHandlers struct {
	store selectorAdminStore
	// catalog is nil when no candidate catalog is configured; candidates
	// and preview then answer 503. The ladder endpoints never touch it.
	catalog selectorCatalog
	// blacklist is nil only in tests without an op store; a read failure is
	// a 500 here, not a silent miss, because this page exists to show the
	// operator the real inputs.
	blacklist blacklistReader
	// favorites are the backend-level rules. The preview is global, so
	// project favorites (merged per trigger) are not applied.
	favorites map[string]board.TierFavorites
	// reasoningEffort echoes llm_endpoint.reasoning_effort on the candidates
	// response so the page can say which effort the gateway pins. Empty when
	// unset.
	reasoningEffort string
	// authEnabled mirrors "multi mode": every endpoint then requires an
	// admin session. In none mode they are open, same trust posture as the
	// model-blacklist endpoints.
	authEnabled bool
}

// selectorLaddersResponse is the GET and PUT /api/admin/selector/ladders
// body. Ladders always carries both roles with all four tiers, and Headroom
// always carries a value: the built-in ladder and headroom when nothing is
// stored (IsDefault true, no UpdatedAt). UpdatedAt is the later of the two
// writes.
type selectorLaddersResponse struct {
	Ladders         map[string]map[string]float64 `json:"ladders"`
	Defaults        map[string]float64            `json:"defaults"`
	Headroom        float64                       `json:"headroom"`
	HeadroomDefault float64                       `json:"headroom_default"`
	IsDefault       bool                          `json:"is_default"`
	UpdatedAt       string                        `json:"updated_at,omitempty"`
}

// selectorLaddersRequest carries the ladders and, optionally, the headroom.
// A Headroom of 0 (or absent) leaves the stored headroom as it is, so a
// client that only knows the ladders never resets it.
type selectorLaddersRequest struct {
	Ladders  map[string]map[string]float64 `json:"ladders"`
	Headroom float64                       `json:"headroom"`
}

func (h *selectorAdminHandlers) gate(w http.ResponseWriter, r *http.Request) bool {
	if !h.authEnabled {
		return true
	}

	return requireAdmin(w, r) != nil
}

// defaultTierBarsWire is selection.DefaultTierBars keyed by tier name.
func defaultTierBarsWire() map[string]float64 {
	defaults := selection.DefaultTierBars()
	out := make(map[string]float64, len(defaults))

	for tier, bar := range defaults {
		out[string(tier)] = bar
	}

	return out
}

// laddersWire renders ladders as wire maps, both roles, all tiers; a nil
// Ladders reads as the built-in ladder through Bars.
func laddersWire(l selection.Ladders) map[string]map[string]float64 {
	out := make(map[string]map[string]float64, 2)

	for _, role := range []selection.Role{selection.RoleCoder, selection.RoleReviewer} {
		bars := l.Bars(role)
		out[string(role)] = make(map[string]float64, len(bars))

		for tier, bar := range bars {
			out[string(role)][string(tier)] = bar
		}
	}

	return out
}

// storedLadders reads the store and resolves it to a response: the built-in
// ladder and headroom when nothing is stored. Ladder rows were validated on
// write, so a ladder that fails validation here is a corrupt store and is an
// error rather than something to paper over with the defaults. The headroom
// is passed through as stored: a corrupt scalar shows on the page as an
// invalid field the operator can type over, which beats locking the page
// behind a 500.
func (h *selectorAdminHandlers) storedLadders(ctx context.Context) (selectorLaddersResponse, error) {
	raw, laddersAt, err := h.store.SelectorLadders(ctx)
	if err != nil {
		return selectorLaddersResponse{}, err
	}

	headroom, headroomAt, err := h.store.SelectorHeadroom(ctx)
	if err != nil {
		return selectorLaddersResponse{}, err
	}

	resp := selectorLaddersResponse{
		Defaults:        defaultTierBarsWire(),
		Headroom:        selection.DefaultPriceHeadroom,
		HeadroomDefault: selection.DefaultPriceHeadroom,
		IsDefault:       len(raw) == 0 && headroom <= 0,
	}

	if headroom > 0 {
		resp.Headroom = headroom
	}

	var ladders selection.Ladders

	if len(raw) > 0 {
		ladders, err = sqlite.ValidateSelectorLadders(raw)
		if err != nil {
			return selectorLaddersResponse{}, err
		}
	}

	resp.Ladders = laddersWire(ladders)

	if at := laterOf(laddersAt, headroomAt); !at.IsZero() {
		resp.UpdatedAt = at.UTC().Format(time.RFC3339)
	}

	return resp, nil
}

func laterOf(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}

// getLadders handles GET /api/admin/selector/ladders.
func (h *selectorAdminHandlers) getLadders(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
		return
	}

	resp, err := h.storedLadders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to read selector ladders", "")

		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// putLadders handles PUT /api/admin/selector/ladders. Both halves are
// validated before either is written, so a bad headroom never leaves a
// saved ladder behind it; the store validates again on its own write path,
// so the sentinels are mapped there too.
func (h *selectorAdminHandlers) putLadders(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
		return
	}

	var req selectorLaddersRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if _, err := sqlite.ValidateSelectorLadders(req.Ladders); err != nil {
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid selector ladders", ladderDetails(err))

		return
	}

	if req.Headroom != 0 {
		if err := sqlite.ValidateSelectorHeadroom(req.Headroom); err != nil {
			writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid selector headroom", headroomDetails(err))

			return
		}
	}

	if err := h.store.PutSelectorLadders(r.Context(), req.Ladders); err != nil {
		if errors.Is(err, sqlite.ErrInvalidLadder) {
			writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid selector ladders", ladderDetails(err))

			return
		}

		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to store selector ladders", "")

		return
	}

	if req.Headroom != 0 {
		if err := h.store.PutSelectorHeadroom(r.Context(), req.Headroom); err != nil {
			if errors.Is(err, sqlite.ErrInvalidHeadroom) {
				writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid selector headroom", headroomDetails(err))

				return
			}

			writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to store selector headroom", "")

			return
		}
	}

	resp, err := h.storedLadders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to read selector ladders", "")

		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// ladderDetails strips the sentinel prefix so the client sees the reason
// (missing role, non-monotone tier) rather than the wrapper.
func ladderDetails(err error) string {
	return strings.TrimPrefix(err.Error(), sqlite.ErrInvalidLadder.Error()+": ")
}

// headroomDetails strips the sentinel prefix so the client sees the reason.
func headroomDetails(err error) string {
	return strings.TrimPrefix(err.Error(), sqlite.ErrInvalidHeadroom.Error()+": ")
}

// selectorCandidatesResponse is the GET /api/admin/selector/candidates body:
// every input the preview feeds the selector, plus the catalog's floor and
// freshness so the page can say what it is showing.
type selectorCandidatesResponse struct {
	Candidates         []selectorCandidateView `json:"candidates"`
	Favorites          []protocol.FavoriteRule `json:"favorites"`
	Blacklist          []string                `json:"blacklist"`
	QualityFloor       float64                 `json:"quality_floor"`
	CatalogRefreshedAt string                  `json:"catalog_refreshed_at"`
	// ReasoningEffort is llm_endpoint.reasoning_effort, the effort the
	// gateway pins; empty when unset.
	ReasoningEffort string `json:"reasoning_effort"`
}

type selectorCandidateView struct {
	Slug                  string  `json:"slug"`
	Creator               string  `json:"creator"`
	CoderPrior            float64 `json:"coder_prior"`
	ReviewerPrior         float64 `json:"reviewer_prior"`
	PromptPricePerTok     float64 `json:"prompt_price_per_tok"`
	CompletionPricePerTok float64 `json:"completion_price_per_tok"`
	ContextWindow         int     `json:"context_window"`
	PriceSource           string  `json:"price_source"`
	// ScoredFrom is the AA slug an automatic join scored the priors from;
	// empty for a model_priors entry and on the OpenRouter leg.
	ScoredFrom string `json:"scored_from"`
}

// selectorPreviewRequest carries the ladders being edited; they are
// validated, never stored. A Headroom of 0 (or absent) means the stored
// headroom, or the built-in one when none is stored.
type selectorPreviewRequest struct {
	Ladders  map[string]map[string]float64 `json:"ladders"`
	Headroom float64                       `json:"headroom"`
}

// selectorPreviewResponse is keyed by tier name; every tier is present.
type selectorPreviewResponse struct {
	Tiers map[string]selectorTierPreview `json:"tiers"`
}

type selectorTierPreview struct {
	Coder    selectorPickReport `json:"coder"`
	Reviewer selectorPickReport `json:"reviewer"`
	Panel    []selectorSeatView `json:"panel"`
}

type selectorPickReport struct {
	Pick   selectorPickView   `json:"pick"`
	Report selectorReportView `json:"report"`
}

// selectorSeatView is one panel seat. Walked marks a seat whose price band
// anchored above the first seat's: every cheaper model at the rung was
// already seated or belonged to a seated vendor.
type selectorSeatView struct {
	selectorPickReport

	Walked bool `json:"walked"`
}

// selectorPickView is selection.Pick on the wire. PricePerTok is the blended
// prompt+completion price of the picked candidate, 0 when the pick is not a
// candidate (OK false).
type selectorPickView struct {
	Model         string  `json:"model"`
	ContextWindow int     `json:"context_window"`
	Role          string  `json:"role"`
	RequestedTier string  `json:"requested_tier"`
	MetTier       string  `json:"met_tier"`
	RequestedBar  float64 `json:"requested_bar"`
	Prior         float64 `json:"prior"`
	HasPrior      bool    `json:"has_prior"`
	Source        string  `json:"source"`
	Duplicate     bool    `json:"duplicate"`
	OK            bool    `json:"ok"`
	PricePerTok   float64 `json:"price_per_tok"`
	// PriceSource is where PricePerTok came from: gateway, aa (the
	// Artificial Analysis list price, when the gateway publishes none),
	// token_costs, or none; empty when the pick is not a candidate.
	PriceSource string `json:"price_source"`
}

type selectorReportView struct {
	Rung        string                    `json:"rung"`
	Bar         float64                   `json:"bar"`
	Pool        []selectorPoolEntryView   `json:"pool"`
	FilteredOut []selectorFilteredOutView `json:"filtered_out"`
}

type selectorPoolEntryView struct {
	Model       string  `json:"model"`
	Prior       float64 `json:"prior"`
	PricePerTok float64 `json:"price_per_tok"`
	Outcome     string  `json:"outcome"`
}

type selectorFilteredOutView struct {
	Reason string   `json:"reason"`
	Models []string `json:"models"`
}

// selectorInputs is what both catalog-backed endpoints read: a sorted copy
// of the candidates, the blacklist, and the snapshot time.
type selectorInputs struct {
	candidates  []protocol.CandidateModel
	provenance  map[string]modelcatalog.CandidateProvenance
	blacklist   []string
	refreshedAt time.Time
}

// inputs gathers the catalog-backed inputs or writes the refusal: 503 for
// no catalog or one that never refreshed, 500 for a blacklist read failure.
func (h *selectorAdminHandlers) inputs(w http.ResponseWriter, r *http.Request) (selectorInputs, bool) {
	if h.catalog == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeCatalogUnavailable, "catalog not available yet", "")

		return selectorInputs{}, false
	}

	ctx := r.Context()
	cands := h.catalog.Candidates(ctx)

	at := h.catalog.LastRefreshed(ctx)
	if at.IsZero() {
		writeError(w, http.StatusServiceUnavailable, ErrCodeCatalogUnavailable, "catalog not available yet", "")

		return selectorInputs{}, false
	}

	in := selectorInputs{candidates: slices.Clone(cands), blacklist: []string{}, refreshedAt: at}

	in.provenance = h.catalog.Provenance(ctx)
	if in.provenance == nil {
		in.provenance = map[string]modelcatalog.CandidateProvenance{}
	}

	if h.blacklist != nil {
		bl, err := h.blacklist.BlacklistedSlugs(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to read model blacklist", "")

			return selectorInputs{}, false
		}

		if bl != nil {
			in.blacklist = bl
		}
	}

	slices.SortFunc(in.candidates, func(a, b protocol.CandidateModel) int { return strings.Compare(a.Slug, b.Slug) })

	return in, true
}

// favoriteRules is the backend-level favorites as wire rules, never nil.
func (h *selectorAdminHandlers) favoriteRules() []protocol.FavoriteRule {
	rules := mergeFavorites(h.favorites, nil)
	if rules == nil {
		return []protocol.FavoriteRule{}
	}

	// mergeFavorites walks maps, so the order it returns varies per call.
	// The page renders these; sort so a refetch never reshuffles them.
	slices.SortFunc(rules, func(a, b protocol.FavoriteRule) int {
		if c := strings.Compare(a.Tier, b.Tier); c != 0 {
			return c
		}

		return strings.Compare(a.Role, b.Role)
	})

	return rules
}

// getCandidates handles GET /api/admin/selector/candidates.
func (h *selectorAdminHandlers) getCandidates(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
		return
	}

	in, ok := h.inputs(w, r)
	if !ok {
		return
	}

	resp := selectorCandidatesResponse{
		Candidates:         make([]selectorCandidateView, 0, len(in.candidates)),
		Favorites:          h.favoriteRules(),
		Blacklist:          in.blacklist,
		QualityFloor:       h.catalog.Floor(),
		CatalogRefreshedAt: in.refreshedAt.UTC().Format(time.RFC3339),
		ReasoningEffort:    h.reasoningEffort,
	}

	for _, c := range in.candidates {
		resp.Candidates = append(resp.Candidates, selectorCandidateView{
			Slug: c.Slug, Creator: c.Creator,
			CoderPrior: c.CoderPrior, ReviewerPrior: c.ReviewerPrior,
			PromptPricePerTok: c.PromptPricePerTok, CompletionPricePerTok: c.CompletionPricePerTok,
			ContextWindow: c.ContextWindow,
			PriceSource:   in.provenance[c.Slug].PriceSource,
			ScoredFrom:    in.provenance[c.Slug].ScoredFrom,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// preview handles POST /api/admin/selector/preview: the single pick per role
// and tier and the review panel the shared selector would produce under the
// request's ladders, against the live catalog. Stateless; cheap enough for
// every debounced drag step.
func (h *selectorAdminHandlers) preview(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r) {
		return
	}

	var req selectorPreviewRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	ladders, err := sqlite.ValidateSelectorLadders(req.Ladders)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid selector ladders", ladderDetails(err))

		return
	}

	headroom := req.Headroom

	if headroom != 0 {
		if err := sqlite.ValidateSelectorHeadroom(headroom); err != nil {
			writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid selector headroom", headroomDetails(err))

			return
		}
	} else {
		stored, _, err := h.store.SelectorHeadroom(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to read selector headroom", "")

			return
		}

		headroom = stored
		if headroom <= 0 {
			headroom = selection.DefaultPriceHeadroom
		}
	}

	in, ok := h.inputs(w, r)
	if !ok {
		return
	}

	sel := selection.New(selection.Input{
		Candidates:    in.candidates,
		Favorites:     h.favoriteRules(),
		Blacklist:     in.blacklist,
		Ladders:       ladders,
		PriceHeadroom: headroom,
	})

	prices := make(map[string]float64, len(in.candidates))
	for _, c := range in.candidates {
		prices[c.Slug] = c.PromptPricePerTok + c.CompletionPricePerTok
	}

	resp := selectorPreviewResponse{Tiers: make(map[string]selectorTierPreview, 4)}

	for tier := range selection.DefaultTierBars() {
		coder, coderRep := sel.SelectByComplexityReport(selection.SelectInput{Role: selection.RoleCoder, Tier: tier})
		reviewer, reviewerRep := sel.SelectByComplexityReport(selection.SelectInput{Role: selection.RoleReviewer, Tier: tier})
		seats := sel.SelectReviewPanelReport(selection.SelectInput{Role: selection.RoleReviewer, Tier: tier}, previewPanelSeats)

		resp.Tiers[string(tier)] = selectorTierPreview{
			Coder:    selectorPickReport{Pick: pickView(coder, prices, in.provenance), Report: reportView(coderRep)},
			Reviewer: selectorPickReport{Pick: pickView(reviewer, prices, in.provenance), Report: reportView(reviewerRep)},
			Panel:    seatViews(seats, prices, in.provenance),
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func pickView(p selection.Pick, prices map[string]float64, prov map[string]modelcatalog.CandidateProvenance) selectorPickView {
	return selectorPickView{
		Model: p.Model, ContextWindow: p.ContextWindow,
		Role: string(p.Role), RequestedTier: string(p.RequestedTier), MetTier: string(p.MetTier),
		RequestedBar: p.RequestedBar, Prior: p.Prior, HasPrior: p.HasPrior,
		Source: p.Source.String(), Duplicate: p.Duplicate, OK: p.OK,
		PricePerTok: prices[p.Model], PriceSource: prov[p.Model].PriceSource,
	}
}

func reportView(rep selection.SelectionReport) selectorReportView {
	out := selectorReportView{
		Rung:        string(rep.Rung),
		Bar:         rep.Bar,
		Pool:        make([]selectorPoolEntryView, 0, len(rep.Pool)),
		FilteredOut: make([]selectorFilteredOutView, 0, len(rep.FilteredOut)),
	}

	for _, e := range rep.Pool {
		out.Pool = append(out.Pool, selectorPoolEntryView{Model: e.Model, Prior: e.Prior, PricePerTok: e.Price, Outcome: string(e.Outcome)})
	}

	for _, f := range rep.FilteredOut {
		out.FilteredOut = append(out.FilteredOut, selectorFilteredOutView{Reason: string(f.Reason), Models: f.Models})
	}

	return out
}

// seatViews renders a panel and marks walked seats. A seat's band anchors
// on the cheapest model in its pool; an anchor above the first seat's means
// the seat paid for diversity or exclusion, which is what an operator is
// looking for when a panel gets expensive.
func seatViews(seats []selection.SeatReport, prices map[string]float64, prov map[string]modelcatalog.CandidateProvenance) []selectorSeatView {
	out := make([]selectorSeatView, 0, len(seats))
	first := -1.0

	for _, s := range seats {
		view := selectorSeatView{selectorPickReport: selectorPickReport{Pick: pickView(s.Pick, prices, prov), Report: reportView(s.Report)}}

		if anchor, ok := poolAnchor(s.Report); ok {
			if first < 0 {
				first = anchor
			}

			view.Walked = anchor > first*(1+1e-6)
		}

		out = append(out, view)
	}

	return out
}

// poolAnchor is the cheapest price in a rung's pool, the number the price
// band is anchored on; false for an empty pool (a duplicated seat).
func poolAnchor(rep selection.SelectionReport) (float64, bool) {
	if len(rep.Pool) == 0 {
		return 0, false
	}

	anchor := rep.Pool[0].Price
	for _, e := range rep.Pool[1:] {
		anchor = min(anchor, e.Price)
	}

	return anchor, true
}
