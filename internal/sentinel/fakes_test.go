package sentinel

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/sentinel/state"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// fakeStore serves fixed settings and windows and counts settings reads, one
// per pass, and every other call. It keeps run rows, heals with the store's
// version guard, and the spend ledger, following the store's rules.
type fakeStore struct {
	mu        sync.Mutex
	settings  state.Settings
	windows   []protocol.SubscriptionUsage
	reads     int
	others    int // calls to every method but SentinelSettings
	panicNext bool
	readErr   error // answered by SubscriptionWindows, so a pass fails after its settings read
	heals     map[int64]state.Heal
	runs      []state.Run
	ledger    []ledgerRow
	refreshes int // RefreshEditSnapshot calls

	findings      []state.Finding
	prs           map[string]state.StandingPR
	outcomeWrites int // SetFindingOutcome calls
}

type ledgerRow struct {
	HealID int64
	Mode   string
	USD    float64
	At     time.Time
	RunID  string
}

func (f *fakeStore) SaveHeal(_ context.Context, h *state.Heal) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	if cur, ok := f.heals[h.ID]; !ok || cur.Version != h.Version {
		return state.ErrHealChanged
	}

	h.Version++
	f.heals[h.ID] = *h

	return nil
}

func (f *fakeStore) putHeal(h state.Heal) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.heals == nil {
		f.heals = make(map[int64]state.Heal)
	}

	f.heals[h.ID] = h
}

func (f *fakeStore) heal(id int64) state.Heal {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.heals[id]
}

func (f *fakeStore) SentinelSettings(context.Context) (state.Settings, time.Time, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.reads++

	if f.panicNext {
		f.panicNext = false

		panic("store failed")
	}

	return f.settings, time.Time{}, "", nil
}

func (f *fakeStore) SubscriptionWindows(context.Context) ([]protocol.SubscriptionUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	return f.windows, f.readErr
}

func (f *fakeStore) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.readErr = err
}

// RecordRunKindStatus records on the card's newest heal with the kind in
// flight, neither answered nor failed, and charges it once per run ID.
func (f *fakeStore) RecordRunKindStatus(_ context.Context, r state.RunKindReport) (int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	if r.Kind == "" || r.RunID == "" || slices.ContainsFunc(f.ledger, func(l ledgerRow) bool { return l.RunID == r.RunID }) {
		return 0, false, nil
	}

	var newest *state.Heal

	for _, h := range f.heals {
		d := h.Dispatch
		if h.Project == r.Project && h.CardID == r.CardID && d.Kind == r.Kind && inFlight(d) &&
			(newest == nil || h.ID > newest.ID) {
			newest = &h
		}
	}

	if newest == nil {
		return 0, false, nil
	}

	newest.Dispatch.AnsweredRunID, newest.Dispatch.Failure = r.RunID, r.Failure

	if r.Result != nil && r.Kind == protocol.RunKindInvestigate {
		if err := json.Unmarshal(r.Result, &newest.Investigation); err != nil {
			return 0, false, err
		}
	}

	if r.Result != nil && r.Kind == protocol.RunKindAnalyse {
		if err := json.Unmarshal(r.Result, &newest.Analysis); err != nil {
			return 0, false, err
		}
	}

	newest.Version++
	f.heals[newest.ID] = *newest

	usd := r.SpendUSD
	if r.ChargeCeiling {
		usd = newest.Dispatch.CeilingUSD
	}

	f.ledger = append(f.ledger, ledgerRow{HealID: newest.ID, Mode: newest.Dispatch.Mode, USD: usd, At: r.At, RunID: r.RunID})

	return newest.ID, true, nil
}

func (f *fakeStore) AddFindings(context.Context, int64, string, string, []protocol.FrictionFinding, time.Time) error {
	return nil
}

func (f *fakeStore) UndecidedRuns(context.Context) ([]state.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	var runs []state.Run

	for _, r := range f.runs {
		if r.Launch.Autonomous && !r.Decided {
			runs = append(runs, r)
		}
	}

	return runs, nil
}

func (f *fakeStore) MarkRunDecided(_ context.Context, project, cardID string, started time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++
	f.markDecided(project, cardID, started)

	return nil
}

func (f *fakeStore) markDecided(project, cardID string, started time.Time) {
	for i, r := range f.runs {
		if r.Project == project && r.CardID == cardID && r.StartedAt.Equal(started) {
			f.runs[i].Decided = true
		}
	}
}

// OpenHeal keeps one heal per run start: a second open returns the first's ID.
// Opening a heal, first or not, marks its run decided.
func (f *fakeStore) OpenHeal(_ context.Context, h state.Heal) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	if f.heals == nil {
		f.heals = make(map[int64]state.Heal)
	}

	if h.Kind == state.HealKindHeal {
		f.markDecided(h.Project, h.CardID, h.RunStarted)
	}

	var maxID int64

	for _, cur := range f.heals {
		if h.Kind == state.HealKindHeal && cur.Kind == state.HealKindHeal && cur.Project == h.Project && cur.CardID == h.CardID &&
			cur.RunStarted.Equal(h.RunStarted) {
			return cur.ID, nil
		}

		maxID = max(maxID, cur.ID)
	}

	h.ID, h.Version = maxID+1, 0
	f.heals[h.ID] = h

	return h.ID, nil
}

func (f *fakeStore) OpenHeals(context.Context) ([]state.Heal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	var heals []state.Heal

	for _, h := range f.heals {
		if h.Kind == state.HealKindHeal && !state.Terminal(h.Stage) {
			heals = append(heals, h)
		}
	}

	slices.SortFunc(heals, func(a, b state.Heal) int { return int(a.ID - b.ID) })

	return heals, nil
}

func (f *fakeStore) InFlightRows(context.Context) ([]state.Heal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	var heals []state.Heal

	for _, h := range f.heals {
		if inFlight(h.Dispatch) {
			heals = append(heals, h)
		}
	}

	slices.SortFunc(heals, func(a, b state.Heal) int { return int(a.ID - b.ID) })

	return heals, nil
}

// isHealKind reports whether the store's heal readers serve a row of kind.
func isHealKind(kind string) bool {
	return kind == state.HealKindHeal || kind == state.HealKindForward
}

// UnqueuedRuns follows the store: only runs that have an ID.
func (f *fakeStore) UnqueuedRuns(context.Context) ([]state.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	var runs []state.Run

	for _, r := range f.runs {
		if r.RunID != "" && !r.AnalysisQueued {
			runs = append(runs, r)
		}
	}

	return runs, nil
}

// QueueAnalysis keeps one analysis row per run ID.
func (f *fakeStore) QueueAnalysis(_ context.Context, h state.Heal) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	if f.heals == nil {
		f.heals = make(map[int64]state.Heal)
	}

	var maxID int64

	dup := false

	for _, cur := range f.heals {
		maxID = max(maxID, cur.ID)
		dup = dup || cur.Kind == state.HealKindAnalysis && cur.Project == h.Project && cur.CardID == h.CardID && cur.RunID == h.RunID
	}

	if !dup {
		h.ID, h.Version = maxID+1, 0
		f.heals[h.ID] = h
	}

	f.markQueued(h.Project, h.CardID, h.RunStarted)

	return nil
}

func (f *fakeStore) MarkAnalysisQueued(_ context.Context, project, cardID string, started time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++
	f.markQueued(project, cardID, started)

	return nil
}

func (f *fakeStore) markQueued(project, cardID string, started time.Time) {
	for i, r := range f.runs {
		if r.Project == project && r.CardID == cardID && r.StartedAt.Equal(started) {
			f.runs[i].AnalysisQueued = true
		}
	}
}

// OpenAnalyses returns the analysis rows at stage analyse, newest first.
func (f *fakeStore) OpenAnalyses(context.Context) ([]state.Heal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	var rows []state.Heal

	for _, h := range f.heals {
		if h.Kind == state.HealKindAnalysis && h.Stage == state.StageAnalyse {
			rows = append(rows, h)
		}
	}

	slices.SortFunc(rows, func(a, b state.Heal) int { return int(b.ID - a.ID) })

	return rows, nil
}

// RunHeal returns the heal of the run that started at started.
func (f *fakeStore) RunHeal(_ context.Context, project, cardID string, started time.Time) (*state.Heal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	for _, h := range f.heals {
		if h.Kind == state.HealKindHeal && h.Project == project && h.CardID == cardID && h.RunStarted.Equal(started) {
			return &h, nil
		}
	}

	return nil, nil //nolint:nilnil // nil, nil means no heal, as the store answers
}

// CardHeals returns the card's rows, newest first.
func (f *fakeStore) CardHeals(_ context.Context, project, cardID string) ([]state.Heal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	var heals []state.Heal

	for _, h := range f.heals {
		if isHealKind(h.Kind) && h.Project == project && h.CardID == cardID {
			heals = append(heals, h)
		}
	}

	slices.SortFunc(heals, func(a, b state.Heal) int { return int(b.ID - a.ID) })

	return heals, nil
}

func (f *fakeStore) LatestHeals(_ context.Context, project string) (map[string]state.Heal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	latest := map[string]state.Heal{}

	for _, h := range f.heals {
		if cur, ok := latest[h.CardID]; isHealKind(h.Kind) && h.Project == project && (!ok || h.ID > cur.ID) {
			latest[h.CardID] = h
		}
	}

	return latest, nil
}

func (f *fakeStore) Charge(_ context.Context, healID int64, mode string, usd float64, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++
	f.ledger = append(f.ledger, ledgerRow{HealID: healID, Mode: mode, USD: usd, At: at})

	return nil
}

func (f *fakeStore) SpentSince(_ context.Context, mode string, since time.Time) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	var usd float64

	for _, l := range f.ledger {
		if l.Mode == mode && !l.At.Before(since) {
			usd += l.USD
		}
	}

	return usd, nil
}

func (f *fakeStore) HealedSince(_ context.Context, since time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	cards := map[string]bool{}

	for _, h := range f.heals {
		if h.Kind == state.HealKindHeal && h.Stage == state.StageHealed && !h.UpdatedAt.Before(since) {
			cards[h.Project+"/"+h.CardID] = true
		}
	}

	return len(cards), nil
}

func (f *fakeStore) CardRunRow(_ context.Context, project, cardID string) (*state.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	for _, r := range f.runs {
		if r.Project == project && r.CardID == cardID {
			return &r, nil
		}
	}

	return nil, nil //nolint:nilnil // nil, nil means no row, as the store answers
}

func (f *fakeStore) RefreshEditSnapshot(_ context.Context, project, cardID string, snap board.EditSnapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++
	f.refreshes++

	for i, r := range f.runs {
		if r.Project == project && r.CardID == cardID {
			f.runs[i].Launch.Snapshot = snap
		}
	}

	return nil
}

func (f *fakeStore) IncrementAttempts(_ context.Context, project, cardID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	for i, r := range f.runs {
		if r.Project == project && r.CardID == cardID {
			f.runs[i].Attempts++
		}
	}

	return nil
}

func (f *fakeStore) IncrementUsageReruns(_ context.Context, project, cardID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	for i, r := range f.runs {
		if r.Project == project && r.CardID == cardID {
			f.runs[i].UsageReruns++
		}
	}

	return nil
}

// putRun replaces the card's run row, as a launch does.
func (f *fakeStore) putRun(r state.Run) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.runs = slices.DeleteFunc(f.runs, func(cur state.Run) bool { return cur.Project == r.Project && cur.CardID == r.CardID })
	f.runs = append(f.runs, r)
}

func (f *fakeStore) run(project, cardID string) state.Run {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, r := range f.runs {
		if r.Project == project && r.CardID == cardID {
			return r
		}
	}

	return state.Run{}
}

func (f *fakeStore) allHeals() []state.Heal {
	f.mu.Lock()
	defer f.mu.Unlock()

	heals := make([]state.Heal, 0, len(f.heals))
	for _, h := range f.heals {
		heals = append(heals, h)
	}

	slices.SortFunc(heals, func(a, b state.Heal) int { return int(a.ID - b.ID) })

	return heals
}

func (f *fakeStore) charges() []ledgerRow {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.ledger)
}

func (f *fakeStore) otherCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.others
}

func (f *fakeStore) passes() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.reads
}

func (f *fakeStore) set(s state.Settings) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.settings = s
}

// fakeChannel maps a backend kind to its effective mode; other kinds get "".
// It keeps no usage.
type fakeChannel map[llmchannel.Kind]llmchannel.Mode

func (f fakeChannel) Effective(kind llmchannel.Kind) llmchannel.Mode { return f[kind] }

func (f fakeChannel) RecordUsage(context.Context, []protocol.SubscriptionUsage) {}

// fakeBackend answers every run kind with err and keeps what it was sent;
// onSend runs before it answers. It lists containers and reports health as
// set, counting each read.
type fakeBackend struct {
	err        error
	onSend     func(protocol.RunKindPayload)
	sent       []protocol.RunKindPayload
	containers []backend.ContainerInfo
	health     protocol.HealthResponse
	lists      int
	healths    int
}

func (f *fakeBackend) RunKind(_ context.Context, p protocol.RunKindPayload) error {
	if f.onSend != nil {
		f.onSend(p)
	}

	f.sent = append(f.sent, p)

	return f.err
}

func (f *fakeBackend) ListContainers(context.Context) ([]backend.ContainerInfo, error) {
	f.lists++

	return f.containers, nil
}

func (f *fakeBackend) Health(context.Context) (protocol.HealthResponse, error) {
	f.healths++

	return f.health, nil
}

// fakeCards serves cards and projects from memory; ListCards filters by
// parent and state only. It keeps every system log entry by card ID, served
// or not, and counts every call; onGet runs before GetCard answers. PatchCard
// and TransitionToAs replace the stored card with an updated copy, as the
// service writes a new file, refuse a stale guard (updated or body), and
// answer a card ID in refuse with its error; PatchCard keeps every input.
type fakeCards struct {
	cards       []*board.Card
	projects    map[string]*board.ProjectConfig
	logged      map[string][]board.ActivityEntry
	calls       int
	onGet       func()
	patches     []patchCall
	refuse      map[string]error
	transitions []string // "<ID> <state> <agent>"
	created     []service.CreateCardInput
}

type patchCall struct {
	ID string
	In service.PatchCardInput
}

func (f *fakeCards) PatchCard(_ context.Context, project, id string, in service.PatchCardInput) (*board.Card, error) {
	f.calls++
	f.patches = append(f.patches, patchCall{ID: id, In: in})

	if err := f.refuse[id]; err != nil {
		return nil, err
	}

	return f.write(project, id, func(c *board.Card) error {
		if in.ExpectUpdated != nil && !c.Updated.Equal(*in.ExpectUpdated) {
			return service.ErrCardChanged
		}

		if in.ExpectBody != nil && c.Body != *in.ExpectBody {
			return service.ErrCardChanged
		}

		if in.Body != nil {
			c.Body = *in.Body
		}

		if in.UpsertSection != nil {
			c.Body = board.UpsertSection(c.Body, in.UpsertSection.Heading, in.UpsertSection.Content)
		}

		if in.Verify != nil {
			c.Verify = in.Verify
		}

		if in.Phase != nil {
			c.Phase = *in.Phase
		}

		if in.State != nil {
			c.State = *in.State
		}

		return nil
	})
}

func (f *fakeCards) TransitionToAs(
	_ context.Context, project, id, state, agentID string, expectUpdated time.Time,
) (*board.Card, error) {
	f.calls++
	f.transitions = append(f.transitions, id+" "+state+" "+agentID)

	if err := f.refuse[id]; err != nil {
		return nil, err
	}

	return f.write(project, id, func(c *board.Card) error {
		if !c.Updated.Equal(expectUpdated) {
			return service.ErrCardChanged
		}

		c.State = state

		return nil
	})
}

// write applies edit to a copy of the stored card and stores the copy one
// second newer.
func (f *fakeCards) write(project, id string, edit func(*board.Card) error) (*board.Card, error) {
	for i, c := range f.cards {
		if c.Project != project || c.ID != id {
			continue
		}

		cp := *c
		if err := edit(&cp); err != nil {
			return nil, err
		}

		cp.Updated = c.Updated.Add(time.Second)
		f.cards[i] = &cp

		return &cp, nil
	}

	return nil, storage.ErrCardNotFound
}

func (f *fakeCards) ClaimedElsewhere(*board.Card) bool {
	f.calls++

	return false
}

func (f *fakeCards) AddSystemLogEntry(_ context.Context, _, id string, e board.ActivityEntry) (*board.Card, error) {
	f.calls++

	if f.logged == nil {
		f.logged = make(map[string][]board.ActivityEntry)
	}

	f.logged[id] = append(f.logged[id], e)

	return nil, nil //nolint:nilnil // the controller never reads the card back
}

func (f *fakeCards) GetCard(_ context.Context, project, id string) (*board.Card, error) {
	f.calls++

	if f.onGet != nil {
		f.onGet()
	}

	for _, c := range f.cards {
		if c.Project == project && c.ID == id {
			return c, nil
		}
	}

	return nil, storage.ErrCardNotFound
}

func (f *fakeCards) ListCards(_ context.Context, project string, filter storage.CardFilter) ([]*board.Card, error) {
	f.calls++

	var out []*board.Card

	for _, c := range f.cards {
		if c.Project == project && (filter.Parent == "" || c.Parent == filter.Parent) &&
			(filter.Label == "" || slices.Contains(c.Labels, filter.Label)) &&
			(filter.State == "" || c.State == filter.State) {
			out = append(out, c)
		}
	}

	return out, nil
}

func (f *fakeCards) GetProject(_ context.Context, project string) (*board.ProjectConfig, error) {
	f.calls++

	if p, ok := f.projects[project]; ok {
		return p, nil
	}

	return nil, storage.ErrProjectNotFound
}

func (f *fakeCards) ListProjects(context.Context) ([]board.ProjectConfig, error) {
	f.calls++

	var out []board.ProjectConfig
	for _, p := range f.projects {
		out = append(out, *p)
	}

	return out, nil
}

// fakePlaybooks serves playbook details by ID.
type fakePlaybooks map[string]*service.PlaybookDetail

func (f fakePlaybooks) Get(_ context.Context, id string) (*service.PlaybookDetail, error) {
	if pb, ok := f[id]; ok {
		return pb, nil
	}

	return nil, storage.ErrPlaybookNotFound
}

// fakeRunner keeps every Stop and Play as "<call> <playbook> <agent>" and
// answers each with err; onCall runs first with the call.
type fakeRunner struct {
	calls  []string
	err    error
	onCall func(call string)
}

func (f *fakeRunner) Stop(_ context.Context, id, agentID string) (*service.PlaybookDetail, error) {
	return nil, f.call("stop " + id + " " + agentID)
}

func (f *fakeRunner) Play(_ context.Context, id, agentID string) (*service.PlaybookDetail, error) {
	return nil, f.call("play " + id + " " + agentID)
}

func (f *fakeRunner) call(c string) error {
	if f.onCall != nil {
		f.onCall(c)
	}

	f.calls = append(f.calls, c)

	return f.err
}

func enabledSettings() state.Settings {
	s := state.DefaultSettings()
	s.Enabled = true

	return s
}

func (f *fakeStore) putFinding(fd state.Finding) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.findings = append(f.findings, fd)
}

func (f *fakeStore) finding(id int64) state.Finding {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, fd := range f.findings {
		if fd.ID == id {
			return fd
		}
	}

	return state.Finding{}
}

// findingsWhere returns the findings keep accepts, in ID order.
func (f *fakeStore) findingsWhere(keep func(state.Finding) bool) []state.Finding {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.others++

	var out []state.Finding

	for _, fd := range f.findings {
		if keep(fd) {
			out = append(out, fd)
		}
	}

	slices.SortFunc(out, func(a, b state.Finding) int { return int(a.ID - b.ID) })

	return out
}

func (f *fakeStore) QueuedFindings(context.Context) ([]state.Finding, error) {
	return f.findingsWhere(func(fd state.Finding) bool { return fd.State == state.FindingQueued }), nil
}

func (f *fakeStore) ProjectGuidance(_ context.Context, project string) ([]state.Finding, error) {
	return f.findingsWhere(func(fd state.Finding) bool {
		return fd.Project == project && fd.Kind == protocol.FindingGuidance &&
			slices.Contains([]string{state.FindingPending, state.FindingProposed, state.FindingMerged, state.FindingRejected}, fd.State)
	}), nil
}

func (f *fakeStore) ProposedFindings(_ context.Context, project string) ([]state.Finding, error) {
	return f.findingsWhere(func(fd state.Finding) bool {
		return fd.Project == project && fd.Kind == protocol.FindingGuidance && fd.State == state.FindingProposed
	}), nil
}

func (f *fakeStore) StandingPRs(context.Context) ([]state.StandingPR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []state.StandingPR
	for _, p := range f.prs {
		out = append(out, p)
	}

	slices.SortFunc(out, func(a, b state.StandingPR) int { return strings.Compare(a.Project, b.Project) })

	return out, nil
}

func (f *fakeStore) StandingPR(_ context.Context, project string) (*state.StandingPR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if p, ok := f.prs[project]; ok {
		return &p, nil
	}

	return nil, nil
}

func (f *fakeStore) PutStandingPR(_ context.Context, p state.StandingPR) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.prs == nil {
		f.prs = map[string]state.StandingPR{}
	}

	f.prs[p.Project] = p

	return nil
}

func (f *fakeStore) CardedSince(_ context.Context, project string, since time.Time) (int, error) {
	return len(f.findingsWhere(func(fd state.Finding) bool {
		return fd.Project == project && fd.State == state.FindingCarded && !fd.UpdatedAt.Before(since)
	})), nil
}

func (f *fakeStore) CardedFindings(_ context.Context, project string) (map[string]bool, error) {
	out := map[string]bool{}

	for _, fd := range f.findingsWhere(func(fd state.Finding) bool {
		return fd.Project == project && fd.State == state.FindingCarded && fd.Output != ""
	}) {
		out[fd.Output] = true
	}

	return out, nil
}

func (f *fakeStore) SetFindingOutcome(_ context.Context, id int64, st, reason, output string, revisionID int64, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.outcomeWrites++

	for i, fd := range f.findings {
		if fd.ID == id {
			f.findings[i].State, f.findings[i].Reason, f.findings[i].Output = st, reason, output
			f.findings[i].RevisionID, f.findings[i].UpdatedAt = revisionID, at

			return nil
		}
	}

	return nil
}

func (f *fakeCards) CreateCard(_ context.Context, project string, in service.CreateCardInput) (*board.Card, error) {
	f.calls++
	f.created = append(f.created, in)

	c := &board.Card{
		ID: strings.ToUpper(project) + "-" + strconv.Itoa(99+len(f.created)), Project: project, Title: in.Title,
		Type: in.Type, State: board.StateTodo, Labels: in.Labels, Body: in.Body,
	}
	f.cards = append(f.cards, c)

	return c, nil
}
