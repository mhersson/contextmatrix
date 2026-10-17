import type {
  BlueprintArtifact,
  BlueprintArtifactRef,
  BlueprintArtifactReview,
  BlueprintPipelineStep,
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

const ALL = [
  NEED_V1,
  SLICES_V1,
  DESIGN_V1,
  DESIGN_V2,
  GROUNDING_V1,
  ACCEPTANCE_V1,
  CARDS_V1,
  READINESS_V2,
  SIGNOFF_V1,
  BOARD_V1,
];

export function findArtifact(stage: string, version: number): BlueprintArtifact {
  const found = ALL.find((a) => a.stage === stage && a.version === version);
  if (!found) throw new Error(`no fixture for ${stage} v${version}`);
  return found;
}
