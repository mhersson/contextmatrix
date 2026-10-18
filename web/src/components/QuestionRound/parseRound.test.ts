import { describe, expect, it } from 'vitest';
import { rawRound } from '../../test-utils/roundFixtures';
import { parseRound } from './parseRound';

const one = (q: Record<string, unknown>) => rawRound({ questions: [q] });

describe('parseRound', () => {
  it('reads questions, options, answers, decisions, chat and the flags', () => {
    const r = parseRound(
      rawRound({
        assumptions: [{ id: 'A1', text: 'One cluster per environment.' }],
        answers: [{ number: 1, option: 1 }, { number: 2, text: 'Loki only' }],
        decisions: [{ id: 'A1', accept: true }],
        chat: [{ from: 'user', text: 'Does Q3 change Q1?' }, { from: 'system_designer', text: 'No.' }],
        research_off: true,
        responder: true,
        closed: true,
      }),
    );
    expect(r).not.toBeNull();
    expect(r!.persona).toBe('system_designer');
    expect(r!.questions.map((q) => q.number)).toEqual([1, 2, 3]);
    expect(r!.questions[0].options[0]).toEqual({
      title: 'Kustomize',
      summary: 'Plain manifests with overlays per environment.',
      pros: ['No templating language', 'Built into kubectl'],
      cons: ['Less suited to publishing a reusable chart'],
      sources: [{ tag: 'S1', title: 'kubectl.docs.kubernetes.io/guides', url: 'https://kubectl.docs.kubernetes.io/guides/' }],
    });
    expect(r!.questions[1].options[0]).toEqual({
      title: 'Grafana + Loki + Fluent Bit',
      summary: 'Collect, store and search logs inside the cluster.',
      pros: [],
      cons: [],
      sources: [],
    });
    expect(r!.questions[1].recommended).toBe(0);
    expect(r!.assumptions).toEqual([{ id: 'A1', text: 'One cluster per environment.' }]);
    expect(r!.answers).toEqual([{ number: 1, option: 1 }, { number: 2, text: 'Loki only' }]);
    expect(r!.decisions).toEqual([{ id: 'A1', accept: true }]);
    expect(r!.chat).toHaveLength(2);
    expect([r!.researchOff, r!.responder, r!.closed]).toEqual([true, true, true]);
  });

  it('never keeps a non-http source url, so it cannot become a link', () => {
    const r = parseRound(
      one({
        number: 1,
        title: 'T',
        text: 'Q?',
        options: [
          {
            title: 'A',
            sources: [
              { tag: 'S1', title: 'evil', url: 'javascript:alert(1)' },
              { tag: 'S2', title: 'data', url: 'data:text/html,x' },
              { tag: 'S3', title: 'ok', url: 'HTTPS://example.com/x' },
              { tag: 'C1', title: 'internal/api/chats.go' },
            ],
          },
        ],
      }),
    );
    expect(r!.questions[0].options[0].sources.map((s) => s.url)).toEqual(['', '', 'HTTPS://example.com/x', '']);
  });

  it('keeps option positions when an option is malformed, so stored answers point at the right one', () => {
    const r = parseRound(one({ number: 4, title: 'T', text: 'Q?', recommended: 2, options: [null, { title: 'B' }] }));
    expect(r!.questions[0].options.map((o) => o.title)).toEqual(['Option 1', 'B']);
    expect(r!.questions[0].recommended).toBe(2);
  });

  it('drops questions without a number or text and keeps the first of a repeated number', () => {
    const r = parseRound(
      rawRound({
        questions: [
          { number: 0, title: 'zero', text: 'Q?', options: [] },
          { number: 5, title: 'blank', text: '  ', options: [] },
          { number: 6, title: 'first', text: 'Q6?', options: [] },
          { number: 6, title: 'second', text: 'Q6 again?', options: [] },
        ],
      }),
    );
    expect(r!.questions.map((q) => q.title)).toEqual(['first']);
  });

  it('clears a recommendation past the options and names an untitled tab Q<n>', () => {
    const r = parseRound(one({ number: 7, text: 'Q?', recommended: 3, options: [{ title: 'A' }] }));
    expect(r!.questions[0].recommended).toBe(0);
    expect(r!.questions[0].title).toBe('Q7');
  });

  it('drops answers and decisions it cannot read', () => {
    const r = parseRound(
      rawRound({
        answers: [{ number: 0, option: 1 }, { number: 2 }, { number: 3, option: -1, text: ' ' }, 'x'],
        decisions: [{ id: '', accept: true }, { id: 'A1' }, { id: 'A2', accept: false }],
      }),
    );
    expect(r!.answers).toEqual([]);
    expect(r!.decisions).toEqual([{ id: 'A2', accept: false }]);
  });

  it('returns null when there is nothing to answer or the body is not an object', () => {
    expect(parseRound(rawRound({ questions: [] }))).toBeNull();
    expect(parseRound('round')).toBeNull();
    expect(parseRound(null)).toBeNull();
  });

  it('accepts a round of assumptions only', () => {
    const r = parseRound(rawRound({ questions: [], assumptions: [{ id: 'A1', text: 'Single region.' }] }));
    expect(r!.questions).toEqual([]);
    expect(r!.assumptions).toHaveLength(1);
  });
});
