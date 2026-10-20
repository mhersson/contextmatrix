import { useEffect, useId, useMemo, useRef } from 'react';
import { MockupChoice } from '../../Mockup/MockupChoice';
import { MockupFrame } from '../../Mockup/MockupFrame';
import { MOCKUP_LABELS } from '../../Mockup/labels';
import { mockupDecided, parseMockup, type MockupSuggestion } from '../../Mockup/mockupData';
import { useMockupPane } from '../../Mockup/mockupPane';
import { useMockupSettings } from '../../Mockup/mockupStore';
import { refreshArtifact } from '../artifactCache';
import { BodyMarkdown } from '../BodyMarkdown';
import { latestArtifact, stageText } from '../stages';
import type { RendererProps } from './types';

export function MockupSheet({ artifact, previous, mode, view }: RendererProps) {
  const pane = useMockupPane();
  const mockup = useMemo(() => parseMockup(artifact.data), [artifact.data]);
  const decided = mockupDecided(view);
  const chatId = pane?.chatId ?? '';
  // Newest comes from the live view on every render, so a revision that lands moves the choice at once.
  const choosable =
    pane?.send !== undefined &&
    pane.promptId !== null &&
    !decided &&
    latestArtifact(view, 'mockup', '')?.version === artifact.version;
  const approved = view.artifacts.some((a) => a.stage === 'mockup' && a.version === artifact.version && a.status === 'approved');
  // The choice sends no artifact_stored frame, and the architect adds `chosen` to the stored version on read.
  const awaitingChosen = approved && mockup !== null && mockup.chosen === null;
  const refreshed = useRef(new Set<string>());
  useEffect(() => {
    const key = `${chatId}/${artifact.version}`;
    if (!awaitingChosen || chatId === '' || refreshed.current.has(key)) return;
    refreshed.current.add(key);
    refreshArtifact(chatId, 'mockup', '', artifact.version);
  }, [awaitingChosen, chatId, artifact.version]);
  // Until `chosen` is known the person's own view stays rather than reverting to the opening settings.
  const [settings, flip] = useMockupSettings(
    chatId,
    artifact.version,
    mockup?.chosen ?? mockup?.settings ?? NO_SETTINGS,
    decided && !awaitingChosen,
  );
  return (
    <>
      <BodyMarkdown body={artifact.body} previous={previous?.body} mode={mode} version={artifact.version} />
      {mockup && (
        <div className="mk-sheet">
          <MockupFrame
            source={mockup.source}
            settings={settings}
            title={`${stageText('mockup').tab} rev ${artifact.version}`}
          />
          {(mockup.suggestions.length > 0 || (choosable && pane)) && (
            <div className="mk-side">
              {mockup.suggestions.length > 0 && (
                <section className="mk-suggestions" aria-label={MOCKUP_LABELS.suggestions}>
                  {mockup.suggestions.map((s) => (
                    <Suggestion
                      key={s.id}
                      suggestion={s}
                      value={settings[s.id] ?? s.default}
                      disabled={decided}
                      onFlip={flip}
                    />
                  ))}
                </section>
              )}
              {choosable && pane && (
                <MockupChoice
                  key={`${pane.promptId}/${artifact.version}`}
                  revision={artifact.version}
                  suggestions={mockup.suggestions}
                  settings={settings}
                  pane={pane}
                />
              )}
            </div>
          )}
        </div>
      )}
    </>
  );
}

const NO_SETTINGS = {};

interface SuggestionProps {
  suggestion: MockupSuggestion;
  value: string;
  disabled: boolean;
  onFlip: (id: string, value: string) => void;
}

function Suggestion({ suggestion: s, value, disabled, onFlip }: SuggestionProps) {
  const titleId = `${useId()}-${s.id}`;
  return (
    <div className="mk-suggestion">
      <span id={titleId} className="mk-title">
        {s.title}
      </span>
      {s.kind === 'switch' ? (
        <button
          type="button"
          role="switch"
          className="mk-switch"
          aria-checked={value === 'on'}
          aria-labelledby={titleId}
          disabled={disabled}
          onClick={() => onFlip(s.id, value === 'on' ? 'off' : 'on')}
        />
      ) : (
        <div role="group" className="mk-seg" aria-labelledby={titleId}>
          {s.options.map((o) => (
            <button
              key={o.value}
              type="button"
              aria-pressed={value === o.value}
              disabled={disabled}
              onClick={() => onFlip(s.id, o.value)}
            >
              {o.label}
            </button>
          ))}
        </div>
      )}
      {s.why !== '' && <p className="mk-why">{s.why}</p>}
    </div>
  );
}
