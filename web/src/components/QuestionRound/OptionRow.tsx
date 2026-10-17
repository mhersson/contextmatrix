import { useId } from 'react';
import type { RoundOption } from './parseRound';

interface OptionRowProps {
  n: number;
  option: RoundOption;
  recommended: boolean;
  selected: boolean;
  expanded: boolean;
  researchOff: boolean;
  onClick: () => void;
}

/** One numbered option; the detail with pros, cons and sources sits outside the button so its links stay focusable. */
export function OptionRow({ n, option, recommended, selected, expanded, researchOff, onClick }: OptionRowProps) {
  const detailId = useId();
  const count = option.sources.length;
  const hasPros = option.pros.length > 0;
  const hasCons = option.cons.length > 0;
  const showSources = count > 0 || (researchOff && (hasPros || hasCons));
  const parts = [
    ...(hasPros ? ['pros'] : []),
    ...(hasCons ? ['cons'] : []),
    ...(count > 0 && !researchOff ? [`${count} ${count === 1 ? 'source' : 'sources'}`] : []),
  ];
  const more = expanded ? 'hide' : parts.join(' · ');
  const hasDetail = hasPros || hasCons || showSources;
  return (
    <div className={`qb-opt${selected ? ' sel' : ''}${expanded ? ' expanded' : ''}`}>
      <button
        type="button"
        className="qb-opt-top"
        aria-pressed={selected}
        aria-expanded={expanded}
        aria-controls={expanded ? detailId : undefined}
        onClick={onClick}
      >
        <span className="qb-n">{n}</span>
        <span>
          <span className="qb-title">{option.title}</span>
          {recommended && <span className="qb-rec">Recommended</span>}
        </span>
        <span className="qb-sum">{option.summary}</span>
        <span className="qb-more" aria-hidden="true">
          {more}
        </span>
      </button>
      {expanded && hasDetail && (
        <div className="qb-detail" id={detailId}>
          {(hasPros || hasCons) && (
            <div className="qb-pc">
              {hasPros && (
                <div className="p">
                  <h5>Pros</h5>
                  <ul>{option.pros.map((p, i) => <li key={i}>{p}</li>)}</ul>
                </div>
              )}
              {hasCons && (
                <div className="c">
                  <h5>Cons</h5>
                  <ul>{option.cons.map((c, i) => <li key={i}>{c}</li>)}</ul>
                </div>
              )}
            </div>
          )}
          {showSources && (
            <div className="qb-src">
              Sources:{' '}
              {researchOff
                ? 'model knowledge'
                : option.sources.map((s, i) =>
                    s.url ? (
                      <a key={i} href={s.url} target="_blank" rel="noopener noreferrer">
                        {s.title}
                      </a>
                    ) : (
                      <span key={i} className="qb-src-text">
                        {s.title}
                      </span>
                    ),
                  )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
