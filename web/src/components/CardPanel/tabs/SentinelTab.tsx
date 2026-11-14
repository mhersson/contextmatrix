import { useId, useState } from 'react';
import type { Card, SentinelCardView } from '../../../types';
import { useCardSentinel } from '../../../hooks/useSentinel';
import { splitFindings } from '../../../lib/sentinel';
import { SentinelSummary } from './sentinel/SentinelSummary';
import { SentinelFacts } from './sentinel/SentinelFacts';
import { SentinelHeals } from './sentinel/SentinelHeals';
import { SentinelFindings } from './sentinel/SentinelFindings';
import { SentinelAnalyses } from './sentinel/SentinelAnalyses';
import { SentinelGuidancePR } from './sentinel/SentinelGuidancePR';

type SubTab = 'heals' | 'findings' | 'analyses' | 'pr';

function SubTabPanel({ tab, view, project }: { tab: SubTab; view: SentinelCardView; project: string }) {
  switch (tab) {
    case 'heals':
      return (
        <div className="flex flex-col gap-3">
          {view.run && <SentinelFacts run={view.run} />}
          <SentinelHeals heals={view.heals} />
        </div>
      );
    case 'findings': {
      const { friction, notes } = splitFindings(view.findings);
      return <SentinelFindings friction={friction} notes={notes} project={project} prURL={view.standing_pr?.url} />;
    }
    case 'analyses':
      return <SentinelAnalyses analyses={view.analyses} />;
    case 'pr':
      return <SentinelGuidancePR pr={view.standing_pr} revision={view.guidance_revision} />;
  }
}

/** Sentinel rail tab: a summary of where Sentinel stands on the card, above one table at a time. */
export function SentinelTab({ card }: { card: Card }) {
  const view = useCardSentinel(card.project, card.id);
  const [chosen, setChosen] = useState<SubTab | null>(null);
  const id = useId();
  if (!view) return null;

  const findings = splitFindings(view.findings).friction.length;
  const current = chosen ?? (view.heals.length > 0 ? 'heals' : 'findings');
  const tabs: { key: SubTab; label: string; count?: number }[] = [
    { key: 'heals', label: 'Heals', count: view.heals.length },
    { key: 'findings', label: 'Findings', count: findings },
    { key: 'analyses', label: 'Analyses', count: view.analyses.length },
    { key: 'pr', label: 'PR' },
  ];

  return (
    <div className="snt-tab flex-1 min-h-0 overflow-y-auto p-4 flex flex-col gap-3" data-testid="sentinel-tab-body">
      {!view.run && (
        <p role="note" className="snt-callout">
          Sentinel acts only on runs this instance launched.
        </p>
      )}
      <SentinelSummary view={view} findings={findings} />
      <div role="tablist" aria-label="Sentinel views" className="snt-subtabs">
        {tabs.map((t) => (
          <button
            key={t.key}
            id={`${id}-${t.key}`}
            type="button"
            role="tab"
            aria-selected={t.key === current}
            aria-controls={`${id}-panel`}
            className="snt-subtab"
            onClick={() => setChosen(t.key)}
          >
            {t.label}
            {t.count !== undefined && <span className="snt-subtab-count">{t.count}</span>}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`${id}-panel`} aria-labelledby={`${id}-${current}`}>
        <SubTabPanel tab={current} view={view} project={card.project} />
      </div>
    </div>
  );
}
