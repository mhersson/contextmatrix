import type {
  BlueprintArtifact,
  BlueprintArtifactRef,
  BlueprintArtifactReview,
  BlueprintPipelineStep,
  BlueprintSlice,
  BlueprintView,
} from '../types';

export function step(
  stage: string,
  persona: string,
  status: BlueprintPipelineStep['status'],
  extra: Partial<BlueprintPipelineStep> = {},
): BlueprintPipelineStep {
  return { stage, persona, gate: false, status, cost_usd: 0, ...extra };
}

export function ref(
  stage: string,
  version: number,
  status: BlueprintArtifactRef['status'],
  extra: Partial<BlueprintArtifactRef> = {},
): BlueprintArtifactRef {
  return { stage, version, created_at: '2026-10-17T10:00:00Z', status, ...extra };
}

/** An architect before protocol v0.28.0: no ids, severity, status or verdict. */
export const DESIGN_REVIEW: BlueprintArtifactReview = {
  reviewer: 'senior_developer',
  score: 4,
  threshold: 4,
  findings: [
    {
      text: 'MarshalIndent plus a hand-written newline duplicates what Encode already does.',
      sent_to: 2,
      addressed_in: 2,
      addressed_by: 'system_designer',
      addressed_note: 'Uses SetIndent on the existing encoder.',
    },
    { text: 'The README update AGENTS.md requires is missing.', sent_to: 2 },
    { text: 'The test should assert key order.', sent_to: 2, addressed_in: 2, addressed_by: 'system_designer' },
  ],
};

/** Design rev 2 waits at the operator's gate: intake done, slicing folded, design rev 1 reviewed and revised. */
export function viewAtDesignGate(): BlueprintView {
  return {
    id: 'S1',
    mode: 'default',
    state: 'waiting',
    seq: 42,
    slices: [],
    pipeline: [
      step('intake', 'product_owner', 'done', { latest_version: 1, cost_usd: 0.03 }),
      step('slicing', 'product_owner', 'folded', {
        gate: true,
        folds_into: 'solution_design',
        latest_version: 1,
        cost_usd: 0.01,
      }),
      step('solution_design', 'system_designer', 'waiting', {
        gate: true,
        policy: 'human',
        latest_version: 2,
        cost_usd: 0.12,
      }),
      step('grounding', 'senior_developer', 'pending'),
      step('acceptance', 'senior_developer', 'pending', { gate: true, policy: 'auto' }),
      step('decomposition', 'senior_developer', 'pending'),
      step('readiness', 'reviewer', 'pending', { max_rounds: 3 }),
      step('signoff', 'you', 'pending', { gate: true }),
      step('emission', 'architect', 'pending'),
    ],
    open: { prompt_id: 'gate:solution_design:v2', kind: 'gate' },
    cost: { spent_usd: 0.16, run_spent_usd: 0, cap_usd: 2, run_cap_usd: 0.5, priced: true },
    artifacts: [
      ref('intake', 1, 'current'),
      ref('slicing', 1, 'folded'),
      ref('solution_design', 1, 'superseded', { review: DESIGN_REVIEW }),
      ref('solution_design', 2, 'at_gate'),
    ],
  };
}

export const THREE_SLICES: BlueprintSlice[] = [
  { id: 'SL1', title: 'Service running with its database' },
  { id: 'SL2', title: 'Logs searchable in the cluster' },
  { id: 'SL3', title: 'Nightly backups and restore' },
];

function sliceSteps(slice: string, statuses: BlueprintPipelineStep['status'][]): BlueprintPipelineStep[] {
  return [
    step('solution_design', 'system_designer', statuses[0], { slice, gate: true, policy: 'human' }),
    step('grounding', 'senior_developer', statuses[1], { slice }),
    step('acceptance', 'senior_developer', statuses[2], { slice, gate: true, policy: 'auto' }),
    step('decomposition', 'senior_developer', statuses[3], { slice }),
  ];
}

/** Three slices: shared decisions and SL1 done, SL2's design rev 1 at your gate, SL3 not started. */
export function viewSliced(): BlueprintView {
  return {
    id: 'S2',
    mode: 'default',
    state: 'waiting',
    seq: 7,
    slices: THREE_SLICES,
    pipeline: [
      step('intake', 'product_owner', 'done', { latest_version: 1 }),
      step('slicing', 'product_owner', 'done', { gate: true, latest_version: 1 }),
      step('shared_decisions', 'system_designer', 'done', { gate: true, policy: 'human', latest_version: 1 }),
      ...sliceSteps('SL1', ['done', 'done', 'done', 'done']),
      ...sliceSteps('SL2', ['waiting', 'pending', 'pending', 'pending']),
      ...sliceSteps('SL3', ['pending', 'pending', 'pending', 'pending']),
      step('readiness', 'reviewer', 'pending', { max_rounds: 3 }),
      step('signoff', 'you', 'pending', { gate: true }),
      step('emission', 'architect', 'pending'),
    ],
    open: { prompt_id: 'gate:solution_design:SL2:v1', kind: 'gate' },
    cost: { spent_usd: 0.84, run_spent_usd: 0, cap_usd: 10, run_cap_usd: 1, priced: true },
    artifacts: [
      ref('intake', 1, 'current'),
      ref('slicing', 1, 'approved', { decided_by: 'you' }),
      ref('shared_decisions', 1, 'approved', { decided_by: 'you' }),
      ref('solution_design', 1, 'approved', { slice: 'SL1', decided_by: 'you' }),
      ref('grounding', 1, 'current', { slice: 'SL1' }),
      ref('acceptance', 1, 'approved', { slice: 'SL1', decided_by: 'automatic' }),
      ref('decomposition', 1, 'current', { slice: 'SL1' }),
      ref('solution_design', 1, 'at_gate', { slice: 'SL2' }),
    ],
  };
}

/** One slice: shared decisions collapse (skipped), slicing folds, the per-slice steps carry SL1 and its design waits at your gate. */
export function viewOneSlice(): BlueprintView {
  return {
    ...viewSliced(),
    slices: [THREE_SLICES[0]],
    pipeline: [
      step('intake', 'product_owner', 'done', { latest_version: 1 }),
      step('slicing', 'product_owner', 'folded', { gate: true, folds_into: 'solution_design', latest_version: 1 }),
      step('shared_decisions', 'system_designer', 'skipped', { gate: true }),
      ...sliceSteps('SL1', ['waiting', 'pending', 'pending', 'pending']),
      step('readiness', 'reviewer', 'pending', { max_rounds: 3 }),
      step('signoff', 'you', 'pending', { gate: true }),
      step('emission', 'architect', 'pending'),
    ],
    open: { prompt_id: 'gate:solution_design:SL1:v1', kind: 'gate' },
    artifacts: [ref('intake', 1, 'current'), ref('slicing', 1, 'folded'), ref('solution_design', 1, 'at_gate', { slice: 'SL1' })],
  };
}

export const DESIGN_SL1_V1: BlueprintArtifact = {
  stage: 'solution_design',
  slice: 'SL1',
  version: 1,
  body: 'CloudNativePG cluster with one replica.',
};

export const DESIGN_SL2_V1: BlueprintArtifact = {
  stage: 'solution_design',
  slice: 'SL2',
  version: 1,
  body: 'Fluent Bit DaemonSet ships pod logs to Loki.',
};

/** Round 1 of the design attempt: rev 1 fails; rev 2 claims two of the findings. */
export const LEDGER_REVIEW_V1: BlueprintArtifactReview = {
  reviewer: 'senior_developer',
  score: 3,
  threshold: 4,
  passed: false,
  round: 1,
  findings: [
    {
      id: 'R1',
      severity: 'major',
      status: 'open',
      text: 'MarshalIndent plus a hand-written newline duplicates what Encode already does.',
      sent_to: 2,
      addressed_in: 2,
      addressed_by: 'system_designer',
      addressed_note: 'Uses SetIndent on the existing encoder.',
    },
    {
      id: 'R2',
      severity: 'critical',
      status: 'open',
      text: 'Nothing says which routes keep compact output.',
      sent_to: 2,
      addressed_in: 2,
      addressed_by: 'system_designer',
      addressed_note: 'Only GET / changes.',
    },
    { id: 'R3', severity: 'minor', status: 'open', text: 'The test should assert key order.', sent_to: 2 },
  ],
};

/** Round 2 re-checks rev 2: two fixed, one still open, one new; it passes, and the open two go to rev 3. */
export const LEDGER_REVIEW_V2: BlueprintArtifactReview = {
  reviewer: 'senior_developer',
  score: 4,
  threshold: 4,
  passed: true,
  round: 2,
  findings: [
    {
      id: 'R1',
      severity: 'major',
      status: 'fixed',
      verified_in: 2,
      text: 'MarshalIndent plus a hand-written newline duplicates what Encode already does.',
    },
    { id: 'R2', severity: 'critical', status: 'fixed', verified_in: 2, text: 'Nothing says which routes keep compact output.' },
    {
      id: 'R3',
      severity: 'minor',
      status: 'still_open',
      verified_in: 2,
      text: 'The test should assert key order.',
      sent_to: 3,
      addressed_in: 3,
      addressed_by: 'system_designer',
      addressed_note: 'Asserts the key order in the handler test.',
    },
    { id: 'R4', severity: 'minor', status: 'open', text: 'The README update AGENTS.md requires is missing.', sent_to: 3 },
  ],
};

/** Round 3, the attempt's last: a new critical finding fails rev 3, which still reaches the gate. */
export const LEDGER_REVIEW_FAILED: BlueprintArtifactReview = {
  reviewer: 'senior_developer',
  score: 3,
  threshold: 4,
  passed: false,
  round: 3,
  findings: [
    { id: 'R3', severity: 'minor', status: 'fixed', verified_in: 3, text: 'The test should assert key order.' },
    { id: 'R4', severity: 'minor', status: 'still_open', verified_in: 3, text: 'The README update AGENTS.md requires is missing.' },
    { id: 'R5', severity: 'critical', status: 'open', text: 'The encoder change also indents the error bodies other routes share.' },
  ],
};

/** Design rev 3 waits at the gate after the attempt's three reviewer rounds, as architects from protocol v0.28.0 send it. */
export function viewAtLedgerGate(): BlueprintView {
  const view = viewAtDesignGate();
  return {
    ...view,
    pipeline: view.pipeline.map((s) => (s.stage === 'solution_design' ? { ...s, latest_version: 3 } : s)),
    open: { prompt_id: 'gate:solution_design:v3', kind: 'gate' },
    artifacts: [
      ref('intake', 1, 'current'),
      ref('slicing', 1, 'folded'),
      ref('solution_design', 1, 'superseded', { review: LEDGER_REVIEW_V1 }),
      ref('solution_design', 2, 'superseded', { review: LEDGER_REVIEW_V2 }),
      ref('solution_design', 3, 'at_gate', { review: LEDGER_REVIEW_FAILED }),
    ],
  };
}

export const NEED_V1: BlueprintArtifact = {
  stage: 'intake',
  version: 1,
  body: '## Problem\n\nThe `/` endpoint returns compact JSON.\n\n## Scope in\n\n- Indented, multi-line JSON.',
  data: {
    slices: 1,
    projects: ['homelander'],
    assumptions: ['Clients ignore whitespace.', { text: 'Two-space indentation.', basis: 'Q1' }],
  },
};

export const SLICES_V1: BlueprintArtifact = {
  stage: 'slicing',
  version: 1,
  body: 'One slice, so slicing folds into the design.',
  data: { slices: [{ id: 'SL1', title: 'Pretty-print GET /', value: 'Readable with plain curl.', projects: ['homelander'] }] },
};

const DEC2 = { id: 'DEC2', text: 'Keep the struct field order', chosen: 'No key sorting', rejected: [], basis: 'S1' };

export const DESIGN_V1: BlueprintArtifact = {
  stage: 'solution_design',
  version: 1,
  body: 'Indent the response by hand.\n\nKeep the field order.',
  data: {
    slices: [
      {
        id: 'SL1',
        projects: ['homelander'],
        decisions: [
          {
            id: 'DEC1',
            text: 'Indent through the existing encoder',
            chosen: 'Call json.MarshalIndent and append a newline',
            rejected: [{ option: 'Middleware for every route', why: 'out of scope' }],
            basis: 'Q1',
          },
          DEC2,
        ],
        contracts: [],
      },
    ],
    assumptions: [],
  },
};

export const DESIGN_V2: BlueprintArtifact = {
  stage: 'solution_design',
  version: 2,
  body: 'Indent the response through the encoder.\n\nKeep the field order.',
  data: {
    slices: [
      {
        id: 'SL1',
        projects: ['homelander'],
        decisions: [
          {
            id: 'DEC1',
            text: 'Indent through the existing encoder',
            chosen: 'Call SetIndent on the encoder the handler already uses',
            rejected: [{ option: 'Middleware for every route', why: 'out of scope' }],
            basis: 'Q1',
          },
          DEC2,
        ],
        contracts: ['None between projects.'],
      },
    ],
    assumptions: [],
  },
};

export const DESIGN_V3: BlueprintArtifact = {
  ...DESIGN_V2,
  version: 3,
  body: 'Indent the response through the encoder.\n\nKeep the field order.\n\nAssert the key order in the handler test.',
};

export const GROUNDING_V1: BlueprintArtifact = {
  stage: 'grounding',
  version: 1,
  body: '## Verified\n\n- The handler is the only place that serialises `/`. [S1]',
  data: {
    projects: {
      homelander: { sha: 'b754015aa1c2', verify_command: 'go test ./...', verify_candidate: '', instruction_file: 'AGENTS.md' },
      emptyrepo: { sha: '', verify_command: '', verify_candidate: 'make check', instruction_file: 'none' },
    },
    verdicts: [
      { id: 'A1', status: 'verified', evidence: '[S1] Encode already ends the body with a newline.' },
      { id: 'A2', status: 'contradicted', evidence: '[S2] The README has no example body.' },
    ],
  },
};

export const ACCEPTANCE_V1: BlueprintArtifact = {
  stage: 'acceptance',
  version: 1,
  body: 'What every card is checked against.',
  data: {
    slices: [
      {
        id: 'SL1',
        criteria: [
          { id: 'AC1', project: 'homelander', text: 'The body has several lines.' },
          { id: 'AC2', project: 'homelander', text: 'Keys keep struct order.' },
        ],
        manual: [{ id: 'MC1', text: 'curl prints one key per line.' }],
      },
    ],
  },
};

export const CARDS_V1: BlueprintArtifact = {
  stage: 'decomposition',
  version: 1,
  body: 'Three cards in homelander.',
  data: {
    title: 'pretty-print-get',
    cards: [
      {
        id: 'D1',
        project: 'homelander',
        title: 'Pretty-print the GET / response',
        type: 'feature',
        priority: 'medium',
        depends_on: [],
        body: 'Where: `main.go`.',
        where: [{ path: 'main.go', symbol: 'sysinfoHandler' }, { path: 'main_test.go', symbol: '' }],
        criteria: ['AC1', 'AC2'],
      },
      {
        id: 'D2',
        project: 'homelander',
        title: 'Add a GET /healthz endpoint',
        type: 'feature',
        priority: 'medium',
        depends_on: [],
        body: 'Health.',
        where: [{ path: 'main.go', symbol: '' }],
        criteria: ['AC3'],
      },
      {
        id: 'D3',
        project: 'homelander',
        title: 'Document both endpoints',
        type: 'task',
        priority: 'low',
        depends_on: ['D1', 'D2'],
        body: 'Docs.',
        where: [{ path: 'README.md', symbol: '' }],
        criteria: [],
      },
    ],
    manual: [{ id: 'M1', text: 'Run curl against the server.', criteria: ['MC1'] }],
    order: ['D1', 'D2', 'D3', 'M1'],
  },
};

export const READINESS_V2: BlueprintArtifact = {
  stage: 'readiness',
  version: 2,
  body: 'Second round.',
  data: {
    cards: [
      { id: 'D1', ready: true, findings: [] },
      { id: 'D3', ready: false, findings: ['The README heading is `### GET /`.'] },
    ],
  },
};

export const SIGNOFF_V1: BlueprintArtifact = {
  stage: 'signoff',
  version: 1,
  body: '',
  data: {
    version: 1,
    title: 'pretty-print-get',
    cards: [
      { id: 'D1', project: 'homelander', title: 'Pretty-print the GET / response', type: 'feature', priority: 'medium', depends_on: [] },
    ],
    order: ['D1', 'M1'],
    manual: [{ id: 'M1', text: 'curl prints one key per line.' }],
    blocking: null,
    warnings: ['One assumption is unverified.'],
    steps: ['set verify command `go test ./...` on project homelander in ContextMatrix'],
    assumptions: [
      { id: 'A1', stage: 'intake', text: 'Clients ignore whitespace.', basis: '', answer: '', status: 'unverified', evidence: '' },
      { id: 'A2', stage: 'intake', text: 'Two-space indentation.', basis: 'Q1', answer: 'Two spaces', status: 'confirmed', evidence: '' },
      { id: 'A3', stage: 'solution_design', text: 'No README example.', basis: 'Q3', answer: '', status: 'assumed', evidence: '' },
      { id: 'A4', stage: 'grounding', text: 'Encode ends with a newline.', basis: '', answer: '', status: 'verified', evidence: '[S1] main.go' },
      { id: 'A5', stage: 'grounding', text: 'The README has an example.', basis: '', answer: '', status: 'contradicted', evidence: '[S2] README.md' },
    ],
  },
};

export const BOARD_V1: BlueprintArtifact = {
  stage: 'emission',
  version: 1,
  body: '',
  data: {
    playbook: 'pretty-print-get',
    cards: [
      {
        draft: 'D1',
        project: 'homelander',
        card: 'HOMELAND-012',
        title: 'Pretty-print the GET / response',
        state: 'in_progress',
        pr: 'https://github.com/example/homelander/pull/7',
      },
      {
        draft: 'D2',
        project: 'homelander',
        card: 'HOMELAND-013',
        title: 'Add a GET /healthz endpoint',
        state: 'todo',
        pr: 'javascript:alert(1)',
      },
      { draft: 'D3', project: 'homelander', card: 'HOMELAND-014', title: 'Document both endpoints' },
    ],
    repos: [{ project: 'homelander', compare_url: 'https://github.com/example/homelander/compare/main...cm' }],
    manual: ['curl prints one key per line.'],
    steps: [],
    cost_usd: 0.41,
    priced: true,
  },
};

/** Rev 1 of the decisions every slice shares; the contract's shape: one list, never `slices`. */
export const SHARED_V1: BlueprintArtifact = {
  stage: 'shared_decisions',
  version: 1,
  body: 'Every slice deploys with Kustomize and logs to Loki.',
  data: {
    decisions: [
      {
        id: 'DEC1',
        text: 'Deploy tooling',
        chosen: 'Kustomize overlays per environment',
        rejected: [{ option: 'Helm chart', why: 'templates are hard to review' }],
        basis: 'Q1',
      },
      { id: 'DEC2', text: 'Observability stack', chosen: 'Grafana, Loki and Fluent Bit in the cluster', rejected: [], basis: 'S1' },
    ],
    contracts: ['Every service logs JSON to stdout.'],
    assumptions: ['The cluster has a default storage class.', { text: 'One namespace per environment.', basis: 'Q2' }],
  },
};

/** Rev 2: DEC1 changes its choice, DEC2 goes, DEC3 is new; no contracts. */
export const SHARED_V2: BlueprintArtifact = {
  stage: 'shared_decisions',
  version: 2,
  body: 'Every slice deploys with Kustomize and ships metrics.',
  data: {
    decisions: [
      {
        id: 'DEC1',
        text: 'Deploy tooling',
        chosen: 'Kustomize overlays per cluster',
        rejected: [{ option: 'Helm chart', why: 'templates are hard to review' }],
        basis: 'Q1',
      },
      { id: 'DEC3', text: 'Metrics', chosen: 'Prometheus Operator', rejected: [], basis: 'S2' },
    ],
    assumptions: [],
  },
};

const ALL = [
  NEED_V1,
  SLICES_V1,
  DESIGN_V1,
  DESIGN_V2,
  DESIGN_V3,
  GROUNDING_V1,
  ACCEPTANCE_V1,
  CARDS_V1,
  READINESS_V2,
  SIGNOFF_V1,
  BOARD_V1,
  DESIGN_SL1_V1,
  DESIGN_SL2_V1,
  SHARED_V1,
  SHARED_V2,
];

export function findArtifact(stage: string, version: number, slice = ''): BlueprintArtifact {
  const found = ALL.find((a) => a.stage === stage && a.version === version && (a.slice ?? '') === slice);
  if (!found) throw new Error(`no fixture for ${stage}${slice ? ` ${slice}` : ''} v${version}`);
  return found;
}

/** A reviewer's account past the summary clamp. */
export const CARD_SUMMARY =
  'Read acceptance rev 2 against the design and the conventions, and ran each criterion against the clean-checkout premise. ' +
  'The dist-dependent checks now build first, the e2e budget holds and the clock seam is named, so the version passes; ' +
  'one minor gap remains and two new findings need a decision before decomposition.';

/** A passed round 2 of 3: two fixed (critical first when unfolded), one still open, two new, one past the finding clamp. */
export const CARD_REVIEW: BlueprintArtifactReview = {
  reviewer: 'tester',
  score: 4,
  threshold: 4,
  passed: true,
  round: 2,
  max_rounds: 3,
  summary: CARD_SUMMARY,
  findings: [
    {
      id: 'R1',
      severity: 'minor',
      status: 'fixed',
      verified_in: 2,
      text: 'AC18 tests only 30 days plus 1 ms as expired.',
      reviewer_note: 'Boundary defined: valid while now <= expires_at.',
      addressed_in: 2,
      addressed_by: 'senior_developer',
      addressed_note: 'Defined the session boundary in the conventions.',
    },
    { id: 'R2', severity: 'critical', status: 'fixed', verified_in: 2, text: 'The e2e budget exceeds the shared rate limit.' },
    {
      id: 'R3',
      severity: 'minor',
      status: 'still_open',
      verified_in: 2,
      text: 'AC29 names no script file or npm script.',
      addressed_in: 2,
      addressed_by: 'senior_developer',
      addressed_note: 'Named the script step.',
    },
    {
      id: 'R4',
      severity: 'minor',
      status: 'open',
      text: 'AC27 is a Vitest test, and so runs inside `npm run verify`, yet it runs `npm run test:e2e` twice. That needs Playwright browser downloads, which the design keeps out of verify.',
    },
    { id: 'R5', severity: 'major', status: 'open', text: 'No criterion checks the UI mapping of 409 identifier_taken to the identifier input.' },
  ],
};

/** Round 1 sends the version back with one new finding and no summary. */
export const SENT_BACK_REVIEW: BlueprintArtifactReview = {
  reviewer: 'senior_developer',
  score: 3,
  threshold: 4,
  passed: false,
  round: 1,
  max_rounds: 3,
  findings: [{ id: 'R1', severity: 'major', status: 'open', text: 'Choose one password hashing algorithm with its cost parameters.' }],
};
