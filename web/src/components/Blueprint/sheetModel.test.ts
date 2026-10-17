import { describe, it, expect } from 'vitest';
import { formatHHMM } from '../../utils/chatTimestamp';
import { ref, viewAtDesignGate } from '../../test-utils/blueprintFixtures';
import { stampFor } from './sheetModel';

describe('stampFor', () => {
  const view = viewAtDesignGate();

  it('stamps superseded, folded, approved and rejected versions', () => {
    expect(stampFor(view, view.artifacts[2])).toEqual({ tone: 'superseded', title: 'Superseded', detail: 'by rev 2' });
    expect(stampFor(view, view.artifacts[1])).toEqual({ tone: 'folded', title: 'Folded', detail: 'into design' });
    const at = '2026-10-17T10:17:00Z';
    expect(stampFor(view, ref('solution_design', 2, 'approved', { decided_by: 'you', decided_at: at }))).toEqual({
      tone: 'approved',
      title: 'Approved',
      detail: `by you, ${formatHHMM(at)}`,
    });
    expect(stampFor(view, ref('solution_design', 2, 'rejected', { decided_by: 'responder' }))?.detail).toBe('by the responder');
  });

  it('leaves undecided versions unstamped', () => {
    expect(stampFor(view, view.artifacts[3])).toBeNull();
    expect(stampFor(view, view.artifacts[0])).toBeNull();
  });
});
