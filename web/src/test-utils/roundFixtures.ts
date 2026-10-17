import type { LogEntry } from '../types';

/** A round body as the architect publishes it (protocol LogRound): three questions, Q1 and Q3 with a recommendation. */
export function rawRound(extra: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    persona: 'system_designer',
    stage: 'solution_design',
    questions: [
      {
        number: 1,
        title: 'Deploy tooling',
        text: 'Helm, Kustomize or both?',
        why: 'Decides how every manifest is written.',
        recommended: 1,
        options: [
          {
            title: 'Kustomize',
            summary: 'Plain manifests with overlays per environment.',
            pros: ['No templating language', 'Built into kubectl'],
            cons: ['Less suited to publishing a reusable chart'],
            sources: [{ tag: 'S1', title: 'kubectl.docs.kubernetes.io/guides', url: 'https://kubectl.docs.kubernetes.io/guides/' }],
          },
          {
            title: 'Helm',
            summary: 'A templated chart with values per environment.',
            pros: ['Packaging and versioned releases'],
            cons: ['Go templates in YAML are hard to review'],
            sources: [{ tag: 'S2', title: 'helm.sh/docs', url: 'https://helm.sh/docs/' }],
          },
        ],
      },
      {
        number: 2,
        title: 'Logging',
        text: 'Own logging stack in the cluster?',
        why: 'Logs vanish with the pod without one.',
        options: [
          { title: 'Grafana + Loki + Fluent Bit', summary: 'Collect, store and search logs inside the cluster.' },
          { title: 'No stack, kubectl logs only', summary: 'Read logs from pods when needed.' },
        ],
      },
      {
        number: 3,
        title: 'Postgres',
        text: 'How should the service database run?',
        why: 'Losing it on a node failure means users log in again.',
        recommended: 1,
        options: [
          { title: 'CloudNativePG operator', summary: 'An operator-managed cluster with failover.' },
          { title: 'Plain StatefulSet', summary: 'One pod with a persistent volume.' },
          { title: 'Managed database outside the cluster', summary: 'A hosted Postgres.' },
        ],
      },
    ],
    ...extra,
  };
}

/** A round frame as CM stores it: a kind "prompt" row holding the {text, prompt} envelope. */
export function roundEntry(seq: number, id: string, round: Record<string, unknown>): LogEntry {
  return {
    ts: '2026-10-17T10:00:00Z',
    card_id: '',
    type: 'text',
    kind: 'prompt',
    seq,
    content: JSON.stringify({ text: 'Questions from the system designer', prompt: { id, kind: 'round', round } }),
  };
}

/** A message typed in a round's chat box as CM stores it: a user row of kind "round_chat" holding the plain text. */
export function roundChatEntry(seq: number, content: string): LogEntry {
  return { ts: '2026-10-17T10:00:00Z', card_id: '', type: 'user', kind: 'round_chat', seq, content };
}
