import { useState } from 'react';
import { MOCKUP_LABELS } from './labels';
import { mockupUseContent, type MockupSuggestion } from './mockupData';
import type { MockupSettings } from './frameProtocol';
import type { MockupPane } from './mockupPane';

interface Props {
  revision: number;
  suggestions: readonly MockupSuggestion[];
  settings: MockupSettings;
  pane: MockupPane;
}

/** Use this design and Skip mockups; the caller shows them on the newest waiting revision only. */
export function MockupChoice({ revision, suggestions, settings, pane }: Props) {
  // After a sent choice both stay off until the architect closes the prompt; the caller keys this per prompt and revision.
  const [phase, setPhase] = useState<'idle' | 'sending' | 'sent'>('idle');
  const [failure, setFailure] = useState<string | null>(null);
  const disabled = phase !== 'idle' || !pane.live || !pane.send || !pane.promptId;

  const choose = async (content: string, action: 'use' | 'skip') => {
    if (!pane.send || !pane.promptId) return;
    setPhase('sending');
    setFailure(null);
    const reply = { prompt_id: pane.promptId, revision, action };
    try {
      await pane.send(content, action === 'use' ? { ...reply, settings: { ...settings } } : reply);
      setPhase('sent');
    } catch (e) {
      setFailure(e instanceof Error ? e.message : 'Failed to send your choice');
      setPhase('idle');
    }
  };

  return (
    <>
      <div className="mk-actions">
        <button
          type="button"
          className="bf-btn-primary bf-btn-sm"
          disabled={disabled}
          onClick={() => void choose(mockupUseContent(revision, suggestions, settings), 'use')}
        >
          {MOCKUP_LABELS.use}
        </button>
        <button
          type="button"
          className="bf-btn-ghost bf-btn-sm"
          disabled={disabled}
          onClick={() => void choose(MOCKUP_LABELS.skip, 'skip')}
        >
          {MOCKUP_LABELS.skip}
        </button>
      </div>
      {failure && (
        <p role="alert" className="bp-load-error">
          {failure}
        </p>
      )}
    </>
  );
}
