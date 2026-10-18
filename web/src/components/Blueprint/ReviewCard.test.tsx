import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import type { BlueprintArtifactRef, BlueprintSlice, LogMarker } from '../../types';
import { CARD_REVIEW, CARD_SUMMARY, SENT_BACK_REVIEW, THREE_SLICES, ref } from '../../test-utils/blueprintFixtures';
import { FINDING_CLAMP_CHARS, SUMMARY_CLAMP_CHARS } from './reviewModel';
import { ReviewCard } from './ReviewCard';
import { BlueprintArtifactsContext, BlueprintSheetContext } from './sheetContext';

const MARKER: LogMarker = { kind: 'review', stage: 'acceptance', slice: 'SL2', version: 2, persona: 'tester' };
const reviewed = (review = CARD_REVIEW, slice = 'SL2') => [ref('acceptance', 2, 'approved', { slice, review })];

function card(marker = MARKER, artifacts: readonly BlueprintArtifactRef[] = reviewed(), slices: readonly BlueprintSlice[] = THREE_SLICES) {
  return (
    <BlueprintSheetContext.Provider value={{ openArtifact: () => {}, slices }}>
      <BlueprintArtifactsContext.Provider value={artifacts}>
        <ReviewCard marker={marker} fallback={<p>Plain verdict.</p>} />
      </BlueprintArtifactsContext.Provider>
    </BlueprintSheetContext.Provider>
  );
}

const group = () => screen.getByRole('group');
const header = () => within(group()).getAllByRole('button')[0];
const openCard = () => fireEvent.click(header());
const finding = (id: string) => screen.getByText(id).closest('li')!;

describe('ReviewCard', () => {
  it('stays collapsed until its header is pressed, outside the live region, and collapses again', () => {
    render(card());
    expect(group()).toHaveAttribute('aria-live', 'off');
    expect(header()).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByText('R3')).not.toBeInTheDocument();
    expect(screen.queryByText(CARD_SUMMARY)).not.toBeInTheDocument();

    openCard();
    expect(header()).toHaveAttribute('aria-expanded', 'true');
    const body = document.getElementById(header().getAttribute('aria-controls') ?? '');
    expect(body).not.toBeNull();
    expect(group()).toContainElement(body);
    expect(within(body!).getByText('R3')).toBeInTheDocument();

    openCard();
    expect(header()).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByText('R3')).not.toBeInTheDocument();
  });

  it('opens on the summary, then the findings', () => {
    render(card());
    openCard();
    const summary = screen.getByText(CARD_SUMMARY);
    expect(summary.compareDocumentPosition(screen.getByText('R3')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('leaves the summary out when the reviewer gave none', () => {
    render(card(MARKER, reviewed({ ...CARD_REVIEW, summary: undefined })));
    openCard();
    expect(screen.queryByText('Summary')).not.toBeInTheDocument();
    expect(screen.getByText('R3')).toBeInTheDocument();
  });

  it('folds the fixed findings until their heading is pressed', () => {
    render(card());
    openCard();
    expect(screen.getByText('R3')).toBeInTheDocument();
    expect(screen.getByText('R5')).toBeInTheDocument();
    expect(screen.queryByText('R1')).not.toBeInTheDocument();
    const fold = screen.getByRole('button', { name: /Fixed/ });
    expect(fold).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(fold);
    expect(fold).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText('R1')).toBeInTheDocument();
    expect(screen.getByText('R2')).toBeInTheDocument();
  });

  it('clamps only long finding text, behind read more', () => {
    expect(CARD_REVIEW.findings[3].text.length).toBeGreaterThan(FINDING_CLAMP_CHARS);
    render(card(MARKER, reviewed({ ...CARD_REVIEW, summary: undefined })));
    openCard();
    expect(within(finding('R5')).queryByRole('button', { name: /read more/ })).not.toBeInTheDocument();
    const more = within(finding('R4')).getByRole('button', { name: /read more/ });
    expect(more).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(more);
    expect(more).toHaveAttribute('aria-expanded', 'true');
  });

  it('clamps a long summary behind read more', () => {
    expect(CARD_SUMMARY.length).toBeGreaterThan(SUMMARY_CLAMP_CHARS);
    const shortOnly = { ...CARD_REVIEW, findings: CARD_REVIEW.findings.filter((f) => f.text.length <= FINDING_CLAMP_CHARS) };
    render(card(MARKER, reviewed(shortOnly)));
    openCard();
    const more = screen.getByRole('button', { name: /read more/ });
    fireEvent.click(more);
    expect(more).toHaveAttribute('aria-expanded', 'true');
  });

  it("shows the reviewer's note and the builder's claim behind details, only where there are some", () => {
    render(card());
    openCard();
    fireEvent.click(screen.getByRole('button', { name: /Fixed/ }));
    const [r1, r3] = [CARD_REVIEW.findings[0], CARD_REVIEW.findings[2]];
    expect(within(finding('R5')).queryByRole('button', { name: /details/ })).not.toBeInTheDocument();
    expect(screen.queryByText(r1.reviewer_note!)).not.toBeInTheDocument();

    const details = within(finding('R1')).getByRole('button', { name: /details/ });
    fireEvent.click(details);
    expect(details).toHaveAttribute('aria-expanded', 'true');
    expect(within(finding('R1')).getByText(r1.reviewer_note!)).toBeInTheDocument();
    expect(within(finding('R1')).getByText(r1.addressed_note!)).toBeInTheDocument();

    fireEvent.click(within(finding('R3')).getByRole('button', { name: /details/ }));
    expect(within(finding('R3')).getByText(r3.addressed_note!)).toBeInTheDocument();
  });

  it('shows the plain line until the view holds the review, then the card', () => {
    const { rerender } = render(card(MARKER, []));
    expect(screen.getByText('Plain verdict.')).toBeInTheDocument();
    expect(screen.queryByRole('group')).not.toBeInTheDocument();

    rerender(card(MARKER, [ref('acceptance', 2, 'reviewing', { slice: 'SL2' })]));
    expect(screen.getByText('Plain verdict.')).toBeInTheDocument();

    rerender(card());
    expect(screen.queryByText('Plain verdict.')).not.toBeInTheDocument();
    expect(group()).toBeInTheDocument();
  });

  it('shows the plain line outside a blueprint pane', () => {
    render(<ReviewCard marker={MARKER} fallback={<p>Plain verdict.</p>} />);
    expect(screen.getByText('Plain verdict.')).toBeInTheDocument();
  });

  it("draws its own slice's review where two slices share a stage and version", () => {
    render(card(MARKER, [...reviewed(SENT_BACK_REVIEW, 'SL1'), ...reviewed(CARD_REVIEW, 'SL2')]));
    openCard();
    expect(screen.getByText(CARD_SUMMARY)).toBeInTheDocument();
    expect(screen.queryByText(SENT_BACK_REVIEW.findings[0].text)).not.toBeInTheDocument();
  });

  it('names the slice only while the blueprint shows two or more slices', () => {
    const { rerender } = render(card());
    expect(header()).toHaveAccessibleName(/SL2/);
    rerender(card({ ...MARKER, slice: 'SL1' }, reviewed(CARD_REVIEW, 'SL1'), [THREE_SLICES[0]]));
    expect(header()).not.toHaveAccessibleName(/SL1/);
  });

  it('has nothing to open without a summary or findings', () => {
    render(card(MARKER, reviewed({ ...CARD_REVIEW, summary: undefined, findings: [] })));
    expect(group()).toBeInTheDocument();
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });
});
