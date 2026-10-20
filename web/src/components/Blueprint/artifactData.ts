// Defensive readers for each artifact's `data`. The data is model-written and
// checked by the architect's validators, but the sheet must never throw on it.

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);
const str = (v: unknown): string => (typeof v === 'string' ? v : '');
const strs = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []);
const records = (v: unknown): Record<string, unknown>[] => (Array.isArray(v) ? v.filter(isRecord) : []);

export interface Assumption {
  id: string;
  text: string;
  /** "Q<n>" when the assumption rests on an answered question. */
  basis: string;
  /** unverified, contradicted, assumed, confirmed or verified, or a stage's own wording; empty when the data carries none. */
  status: string;
  evidence: string;
  /** The answer to the question the basis names, on the sign-off. */
  answer: string;
  /** The stage that stated the assumption, on the sign-off. */
  stage: string;
}

function parseAssumption(a: unknown): Assumption | null {
  if (typeof a === 'string') return a.trim() ? { id: '', text: a, basis: '', status: '', evidence: '', answer: '', stage: '' } : null;
  if (!isRecord(a) || !str(a.text)) return null;
  return {
    id: str(a.id),
    text: str(a.text),
    basis: str(a.basis),
    status: str(a.status),
    evidence: str(a.evidence),
    answer: str(a.answer),
    stage: str(a.stage),
  };
}

export function parseAssumptions(v: unknown): Assumption[] {
  if (!Array.isArray(v)) return [];
  return v.flatMap((a): Assumption[] => {
    const parsed = parseAssumption(a);
    return parsed ? [parsed] : [];
  });
}

export interface ConsequenceAccepted {
  /** The architect's responder constant: "human" (or empty) is you, anything else is named as given. */
  by: string;
  round: number;
}

export interface SignoffAssumption extends Assumption {
  /** The plain consequence of the decision, on a plain-language blueprint. */
  consequence: string;
  accepted: ConsequenceAccepted | null;
  /** The id of the answer that replaced this superseded responder answer. */
  supersededBy: string;
}

export function parseSignoffAssumptions(v: unknown): SignoffAssumption[] {
  if (!Array.isArray(v)) return [];
  return v.flatMap((a): SignoffAssumption[] => {
    const base = parseAssumption(a);
    if (!base) return [];
    if (!isRecord(a)) return [{ ...base, consequence: '', accepted: null, supersededBy: '' }];
    const marker = a.consequence_accepted;
    const accepted =
      isRecord(marker) && typeof marker.round === 'number' && Number.isInteger(marker.round) && marker.round > 0
        ? { by: str(marker.by), round: marker.round }
        : null;
    return [{ ...base, consequence: str(a.consequence), accepted, supersededBy: str(a.superseded_by) }];
  });
}

export const CHANGED_SINCE_ACCEPTED = 'changed since you accepted it';

export function acceptedLabel(a: ConsequenceAccepted): string {
  const who = a.by === 'human' || a.by === '' ? 'you' : a.by;
  return `consequence accepted by ${who}, round ${a.round}`;
}

export const supersededLabel = (id: string): string => `superseded by ${id}`;

export interface PlainSummary {
  build: string[];
  canDo: string[];
  accepted: { id: string; text: string; changed: boolean }[];
}

export function parsePlainSummary(v: unknown): PlainSummary | null {
  if (!isRecord(v)) return null;
  const summary: PlainSummary = {
    build: strs(v.build),
    canDo: strs(v.can_do),
    accepted: records(v.accepted).flatMap((a) =>
      str(a.text) ? [{ id: str(a.id), text: str(a.text), changed: a.changed === true }] : [],
    ),
  };
  return summary.build.length + summary.canDo.length + summary.accepted.length > 0 ? summary : null;
}

/** The assumptions a stage's data states beside its other fields. */
export function stageAssumptions(data: unknown): Assumption[] {
  return isRecord(data) ? parseAssumptions(data.assumptions) : [];
}

export interface Outcome {
  id: string;
  text: string;
  /** The quality the outcome states measurably (performance, security, ...); empty for a plain outcome. */
  quality: string;
}

/** The measurable outcomes a stage's data states; none on a version stored before outcomes existed. */
export function stageOutcomes(data: unknown): Outcome[] {
  if (!isRecord(data)) return [];
  return records(data.outcomes).flatMap((o): Outcome[] =>
    str(o.text) ? [{ id: str(o.id), text: str(o.text), quality: str(o.quality) }] : [],
  );
}

export type ChipTone = 'yellow' | 'red' | 'purple' | 'blue' | 'green' | 'orange' | 'grey';

/** Source tags such as S1 cited in a text. */
export function sourceTags(text: string): string[] {
  return [...new Set(text.match(/\bS\d+\b/g) ?? [])];
}

/** Evidence text without its leading "[S1]" style tags. */
export function withoutTags(text: string): string {
  return text.replace(/^\s*(?:\[S\d+(?:,\s*S\d+)*\]\s*)+/, '').trim();
}

/** The status alone: the basis shows apart. A spelled-out status shows its first clause. */
export function assumptionChip(a: Assumption): { tone: ChipTone; label: string } | null {
  const status = a.status.split(',')[0].trim();
  switch (status) {
    case '':
      return null;
    case 'unverified':
    case 'unconfirmed':
      return { tone: 'yellow', label: status };
    case 'contradicted':
      return { tone: 'red', label: status };
    case 'assumed':
      return { tone: 'purple', label: 'assumed by responder' };
    case 'confirmed':
      return { tone: 'blue', label: 'confirmed by you' };
    case 'verified':
      return { tone: 'green', label: status };
    default:
      return { tone: 'grey', label: status };
  }
}

// A round answer matches first, so round2.A1 stays one reference.
const BASIS_REF = /\b(?:round\d+\.A\d+|[QSAO]\d+)\b/g;

/** Question (Q1), round answer (round2.A1), source (S1), assumption (A3) and outcome (O1) references in a basis, and whatever else it says. */
export function basisRefs(basis: string): { refs: string[]; rest: string } {
  const refs = [...new Set(basis.match(BASIS_REF) ?? [])];
  const rest = basis
    .replace(BASIS_REF, '')
    .replace(/[\s,;]+/g, ' ')
    .trim();
  return { refs, rest };
}

/** A source reads green, a question or round answer blue, a stated assumption or an outcome grey. */
export function refTone(ref: string): ChipTone {
  if (ref.startsWith('S')) return 'green';
  return ref.startsWith('A') || ref.startsWith('O') ? 'grey' : 'blue';
}

export interface NeedData {
  projects: string[];
  assumptions: Assumption[];
}

export function parseNeed(data: unknown): NeedData | null {
  if (!isRecord(data)) return null;
  return { projects: strs(data.projects), assumptions: parseAssumptions(data.assumptions) };
}

export interface Slice {
  id: string;
  title: string;
  value: string;
  projects: string[];
  dependsOn: string[];
}

export function parseSlices(data: unknown): Slice[] | null {
  if (!isRecord(data)) return null;
  return records(data.slices).map((s) => ({
    id: str(s.id),
    title: str(s.title),
    value: str(s.value),
    projects: strs(s.projects),
    dependsOn: strs(s.depends_on),
  }));
}

export interface Decision {
  id: string;
  text: string;
  chosen: string;
  rejected: { option: string; why: string }[];
  basis: string;
}

export interface DesignSlice {
  id: string;
  projects: string[];
  decisions: Decision[];
  contracts: string[];
}

function parseDecision(d: Record<string, unknown>): Decision {
  return {
    id: str(d.id),
    text: str(d.text),
    chosen: str(d.chosen),
    basis: str(d.basis),
    rejected: records(d.rejected).map((r) => ({ option: str(r.option), why: str(r.why) })),
  };
}

export function parseDesign(data: unknown): DesignSlice[] | null {
  if (!isRecord(data)) return null;
  return records(data.slices).map((s) => ({
    id: str(s.id),
    projects: strs(s.projects),
    contracts: strs(s.contracts),
    decisions: records(s.decisions).map(parseDecision),
  }));
}

export interface SharedDecisionsData {
  decisions: Decision[];
  contracts: string[];
  assumptions: Assumption[];
}

/** The decisions every slice builds on: the design's decision object, as one list. */
export function parseSharedDecisions(data: unknown): SharedDecisionsData | null {
  if (!isRecord(data)) return null;
  return {
    decisions: records(data.decisions).map(parseDecision),
    contracts: strs(data.contracts),
    assumptions: parseAssumptions(data.assumptions),
  };
}

export interface GroundingProject {
  name: string;
  sha: string;
  verifyCommand: string;
  verifyCandidate: string;
  instructionFile: string;
}

export interface Verdict {
  id: string;
  status: string;
  evidence: string;
}

export interface GroundingData {
  projects: GroundingProject[];
  verdicts: Verdict[];
}

export function parseGrounding(data: unknown): GroundingData | null {
  if (!isRecord(data)) return null;
  const projects = isRecord(data.projects)
    ? Object.entries(data.projects)
        .flatMap(([name, p]): GroundingProject[] =>
          isRecord(p)
            ? [
                {
                  name,
                  sha: str(p.sha),
                  verifyCommand: str(p.verify_command),
                  verifyCandidate: str(p.verify_candidate),
                  instructionFile: str(p.instruction_file),
                },
              ]
            : [],
        )
        .sort((a, b) => a.name.localeCompare(b.name))
    : [];
  const verdicts = records(data.verdicts).map((v) => ({ id: str(v.id), status: str(v.status), evidence: str(v.evidence) }));
  return { projects, verdicts };
}

export interface Criterion {
  id: string;
  project: string;
  text: string;
  /** The outcomes the criterion proves (O1, ...); none before outcomes existed. */
  outcomes: string[];
}

export interface ManualCheck {
  id: string;
  text: string;
  /** The acceptance manual checks a playbook step covers. */
  criteria: string[];
}

/** Display text per id, cards first and manual checks after. */
export function labelsById(cards: { id: string; title: string }[], manual: ManualCheck[]): Map<string, string> {
  return new Map<string, string>([
    ...cards.map((c): [string, string] => [c.id, c.title]),
    ...manual.map((m): [string, string] => [m.id, m.text]),
  ]);
}

export interface AcceptanceCheck extends ManualCheck {
  /** The outcomes the check proves, when it names any. */
  outcomes: string[];
}

/** One condition of the pack's edge-case list: settled by this slice's criteria or checks, or not applicable. */
export interface SweepEntry {
  condition: string;
  settledBy: string[];
  /** Why the condition does not apply; empty when it is settled. */
  notApplicable: string;
}

export interface AcceptanceSlice {
  id: string;
  criteria: Criterion[];
  manual: AcceptanceCheck[];
  /** One entry per edge-case condition; none before the sweep existed. */
  sweep: SweepEntry[];
}

function parseManual(v: unknown): ManualCheck[] {
  return records(v).map((m) => ({ id: str(m.id), text: str(m.text), criteria: strs(m.criteria) }));
}

export function parseAcceptance(data: unknown): AcceptanceSlice[] | null {
  if (!isRecord(data)) return null;
  return records(data.slices).map((s) => ({
    id: str(s.id),
    criteria: records(s.criteria).map((c) => ({ id: str(c.id), project: str(c.project), text: str(c.text), outcomes: strs(c.outcomes) })),
    manual: records(s.manual).map((m) => ({ id: str(m.id), text: str(m.text), criteria: strs(m.criteria), outcomes: strs(m.outcomes) })),
    sweep: records(s.sweep).flatMap((e): SweepEntry[] =>
      str(e.condition) ? [{ condition: str(e.condition), settledBy: strs(e.settled_by), notApplicable: str(e.not_applicable) }] : [],
    ),
  }));
}

export interface CardPreview {
  id: string;
  project: string;
  title: string;
  type: string;
  priority: string;
  dependsOn: string[];
  body: string;
  /** Code places the card names. */
  places: number;
  /** Acceptance criteria the card covers. */
  criteria: number;
}

export interface CardsData {
  title: string;
  cards: CardPreview[];
  manual: ManualCheck[];
  order: string[];
}

export function parseCards(data: unknown): CardsData | null {
  if (!isRecord(data)) return null;
  return {
    title: str(data.title),
    cards: records(data.cards).map((c) => ({
      id: str(c.id),
      project: str(c.project),
      title: str(c.title),
      type: str(c.type),
      priority: str(c.priority),
      dependsOn: strs(c.depends_on),
      body: str(c.body),
      places: Array.isArray(c.where) ? c.where.length : 0,
      criteria: strs(c.criteria).length,
    })),
    manual: parseManual(data.manual),
    order: strs(data.order),
  };
}

export interface ReadinessCard {
  id: string;
  ready: boolean;
  findings: string[];
}

export function parseReadiness(data: unknown): ReadinessCard[] | null {
  if (!isRecord(data)) return null;
  return records(data.cards).map((c) => ({ id: str(c.id), ready: c.ready === true, findings: strs(c.findings) }));
}

export interface CrossFinding {
  id: string;
  /** critical, major or minor; any other value shows neutrally. */
  severity: string;
  text: string;
  /** The ids the finding names: decisions, criteria, cards, assumptions, outcomes or slices. */
  refs: string[];
}

// A Map, so a severity named after an Object prototype key still ranks last.
const SEVERITY_RANK = new Map([
  ['critical', 0],
  ['major', 1],
  ['minor', 2],
]);

/** Problems between artifacts, critical first; none on a version stored before readiness judged the whole blueprint. */
export function crossFindings(data: unknown): CrossFinding[] {
  if (!isRecord(data)) return [];
  const rank = (f: CrossFinding) => SEVERITY_RANK.get(f.severity) ?? SEVERITY_RANK.size;
  return records(data.cross)
    .flatMap((f): CrossFinding[] =>
      str(f.text) ? [{ id: str(f.id), severity: str(f.severity), text: str(f.text), refs: strs(f.refs) }] : [],
    )
    .sort((a, b) => rank(a) - rank(b));
}

export interface SignoffCard {
  id: string;
  project: string;
  title: string;
  type: string;
  priority: string;
  dependsOn: string[];
  /** The instruction file the card writes, when it is the project's instruction card. */
  writes: string;
}

export interface SignoffData {
  title: string;
  cards: SignoffCard[];
  order: string[];
  manual: ManualCheck[];
  blocking: string[];
  warnings: string[];
  steps: string[];
  /** What will happen without a step from you. */
  notices: string[];
  assumptions: SignoffAssumption[];
  /** The plain-language summary the sign-off opens with; null on a blueprint without one. */
  plainSummary: PlainSummary | null;
  /** An older blueprint's assumptions, given as text instead of a list. */
  assumptionsProse: string;
  /** Set only when the preflight could not run. */
  preflightError: string;
  /** The judged readiness version's findings across the blueprint, critical first. */
  cross: CrossFinding[];
  /** Your reason for approving over a critical finding; set once approved. */
  overrideReason: string;
}

export function parseSignoff(data: unknown): SignoffData | null {
  if (!isRecord(data)) return null;
  return {
    title: str(data.title),
    cards: records(data.cards).map((c) => ({
      id: str(c.id),
      project: str(c.project),
      title: str(c.title),
      type: str(c.type),
      priority: str(c.priority),
      dependsOn: strs(c.depends_on),
      writes: str(c.writes),
    })),
    order: strs(data.order),
    manual: parseManual(data.manual),
    blocking: strs(data.blocking),
    warnings: strs(data.warnings),
    steps: strs(data.steps),
    notices: strs(data.notices),
    assumptions: parseSignoffAssumptions(data.assumptions),
    plainSummary: parsePlainSummary(data.plain_summary),
    assumptionsProse: str(data.assumptions),
    preflightError: str(data.preflight_error),
    cross: crossFindings(data),
    overrideReason: str(data.override_reason),
  };
}

export interface BoardCard {
  /** The draft card id (D1). */
  draft: string;
  project: string;
  /** The ContextMatrix card id. */
  card: string;
  title: string;
  /** Empty until the watch has seen the card. */
  state: string;
  pr: string;
  /** Empty on a board written before cards carried a type. */
  type: string;
  priority: string;
}

export interface BoardData {
  /** Empty for a single card. */
  playbook: string;
  /** In playbook order. */
  cards: BoardCard[];
  repos: { project: string; compareUrl: string }[];
  manual: string[];
  steps: string[];
  /** What will happen without a step from you. */
  notices: string[];
  costUsd: number | null;
  /** False when any architect run was unpriced. */
  priced: boolean;
}

export function parseBoard(data: unknown): BoardData | null {
  if (!isRecord(data)) return null;
  return {
    playbook: str(data.playbook),
    cards: records(data.cards).map((c) => ({
      draft: str(c.draft),
      project: str(c.project),
      card: str(c.card),
      title: str(c.title),
      state: str(c.state),
      pr: str(c.pr),
      type: str(c.type),
      priority: str(c.priority),
    })),
    repos: records(data.repos).map((r) => ({ project: str(r.project), compareUrl: str(r.compare_url) })),
    manual: strs(data.manual),
    steps: strs(data.steps),
    notices: strs(data.notices),
    costUsd: typeof data.cost_usd === 'number' ? data.cost_usd : null,
    priced: data.priced !== false,
  };
}

/** The URL when it is http(s), else null: never link a model-written javascript: or data: URL. */
export function httpUrl(url: string): string | null {
  const trimmed = url.trim();
  return /^https?:\/\//i.test(trimmed) ? trimmed : null;
}
