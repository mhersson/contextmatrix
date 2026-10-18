import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { rawRound } from '../../test-utils/roundFixtures';
import { stageText } from '../Blueprint/stages';
import { parseRound } from './parseRound';
import { RoundSummary } from './RoundSummary';

const answered = { answers: [{ number: 1, option: 1 }, { number: 2, text: 'Loki only' }] };
const assumptions = {
  assumptions: [
    { id: 'env', text: 'One cluster per environment.' },
    { id: 'tls', text: 'TLS ends at the ingress.' },
    { id: 'dns', text: 'DNS is managed outside the cluster.' },
  ],
  decisions: [
    { id: 'env', accept: true },
    { id: 'tls', accept: false },
  ],
};

function renderSummary(extra: Record<string, unknown> = {}, superseded = false) {
  const round = parseRound(rawRound({ ...answered, ...assumptions, closed: true, ...extra }))!;
  return render(<RoundSummary round={round} superseded={superseded} />);
}

const card = () => screen.getByRole('group', { name: /system designer/ });
const header = () => within(card()).getAllByRole('button')[0];

describe('RoundSummary', () => {
  it('stays collapsed until its header is pressed, and collapses again', () => {
    renderSummary();
    expect(header()).toHaveAttribute('aria-expanded', 'false');
    expect(within(card()).queryAllByRole('listitem')).toHaveLength(0);
    expect(screen.queryByText(/Loki only/)).not.toBeInTheDocument();

    fireEvent.click(header());
    expect(header()).toHaveAttribute('aria-expanded', 'true');
    expect(within(card()).getAllByRole('listitem')).toHaveLength(6);

    fireEvent.click(header());
    expect(header()).toHaveAttribute('aria-expanded', 'false');
    expect(within(card()).queryAllByRole('listitem')).toHaveLength(0);
  });

  it('toggles from a native, focusable button', () => {
    renderSummary();
    const toggle = header();
    expect(toggle.tagName).toBe('BUTTON');
    expect(toggle).toHaveAttribute('type', 'button');
    toggle.focus();
    expect(toggle).toHaveFocus();
  });

  it('ties the header to the content it opens', () => {
    renderSummary();
    expect(header()).not.toHaveAttribute('aria-controls');
    fireEvent.click(header());
    const body = document.getElementById(header().getAttribute('aria-controls') ?? '');
    expect(body).not.toBeNull();
    expect(card()).toContainElement(body);
    expect(within(body!).getAllByRole('listitem')).toHaveLength(6);
  });

  it('names the asking stage in the header', () => {
    renderSummary();
    expect(header()).toHaveAccessibleName(new RegExp(stageText('solution_design').name));
  });

  it('puts each stored answer under its own question and leaves an unanswered one without an answer', () => {
    renderSummary();
    fireEvent.click(header());
    const [q1, q2, q3] = within(card()).getAllByRole('listitem');
    expect(q1.textContent).toMatch(/Deploy tooling.*Helm, Kustomize or both\?.*Kustomize/);
    expect(q2.textContent).toMatch(/Own logging stack in the cluster\?.*Loki only/);
    expect(q3).toHaveTextContent('How should the service database run?');
    for (const option of ['CloudNativePG operator', 'Plain StatefulSet', 'Managed database outside the cluster']) {
      expect(q3).not.toHaveTextContent(option);
    }
  });

  it('attributes the answers to the responder on a responder round', () => {
    const { unmount } = renderSummary();
    fireEvent.click(header());
    const mine = within(card()).getAllByRole('listitem')[0].textContent;
    unmount();
    renderSummary({ responder: true });
    fireEvent.click(header());
    expect(within(card()).getAllByRole('listitem')[0].textContent).not.toBe(mine);
  });

  it('gives every assumption its own decision', () => {
    renderSummary();
    fireEvent.click(header());
    const rows = within(card()).getAllByRole('listitem').slice(3);
    const texts = ['One cluster per environment.', 'TLS ends at the ingress.', 'DNS is managed outside the cluster.'];
    const statuses = rows.map((row, i) => {
      expect(row).toHaveTextContent(texts[i]);
      return (row.textContent ?? '').replace(texts[i], '').replace(`A${i + 1}`, '').trim();
    });
    expect(statuses.every((s) => s !== '')).toBe(true);
    expect(new Set(statuses).size).toBe(3);
  });

  it('never shows an assumption id', () => {
    renderSummary();
    fireEvent.click(header());
    for (const id of ['env', 'tls', 'dns']) expect(within(card()).queryByText(new RegExp(`\\b${id}\\b`))).not.toBeInTheDocument();
  });

  it('lists only assumptions for a round without questions', () => {
    renderSummary({ questions: [], answers: [] });
    fireEvent.click(header());
    expect(within(card()).getAllByRole('listitem')).toHaveLength(3);
  });

  it('gives every count of stored answers its own header', () => {
    const headers = [0, 1, 2, 3].map((n) => {
      const { unmount } = renderSummary({ answers: [1, 2, 3].slice(0, n).map((number) => ({ number, option: 1 })) });
      const text = header().textContent;
      unmount();
      return text;
    });
    expect(new Set(headers).size).toBe(4);
  });

  it('gives every mix of assumption decisions its own header', () => {
    const mixes = [
      [true, true, true],
      [true, true, false],
      [true, false, false],
      [true, true, null],
      [null, null, null],
      [false, null, null],
    ];
    const headers = mixes.map((mix) => {
      const decisions = mix.flatMap((accept, i) => (accept === null ? [] : [{ id: assumptions.assumptions[i].id, accept }]));
      const { unmount } = renderSummary({ decisions });
      const text = header().textContent;
      unmount();
      return text;
    });
    expect(new Set(headers).size).toBe(mixes.length);
  });

  it('tells a superseded round apart from a closed one in the header', () => {
    const { unmount } = renderSummary();
    const closed = header().textContent;
    unmount();
    renderSummary({}, true);
    expect(header().textContent).not.toBe(closed);
  });

  it('keeps the round thread collapsed inside the expanded card', () => {
    const chat = [
      { from: 'user', text: 'Why not Helm?' },
      { from: 'system_designer', text: 'Helm suits a published chart.' },
    ];
    renderSummary({ chat });
    expect(within(card()).getAllByRole('button')).toHaveLength(1);

    fireEvent.click(header());
    const thread = within(card()).getAllByRole('button')[1];
    expect(thread).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByText(/Helm suits a published chart\./)).not.toBeInTheDocument();

    fireEvent.click(thread);
    expect(thread).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText(/Helm suits a published chart\./)).toBeInTheDocument();
  });
});
