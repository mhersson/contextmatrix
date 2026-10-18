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
  onToggle: () => void;
}

/** One numbered option; the details toggle is a sibling of the row button (no nested buttons) and the detail sits outside both so its links stay focusable. */
export function OptionRow({ n, option, recommended, selected, expanded, researchOff, onClick, onToggle }: OptionRowProps) {
  const detailId = useId();
  // With research off a web source comes from model knowledge; a code source (no URL) was read with the code tools.
  const sources = researchOff ? option.sources.filter((s) => !s.url) : option.sources;
  const count = sources.length;
  const hasPros = option.pros.length > 0;
  const hasCons = option.cons.length > 0;
  const showSources = option.sources.length > 0 || (researchOff && (hasPros || hasCons));
  const parts = [
    ...(hasPros ? ['pros'] : []),
    ...(hasCons ? ['cons'] : []),
    ...(count > 0 ? [`${count} ${count === 1 ? 'source' : 'sources'}`] : []),
  ];
  const more = expanded ? 'hide' : parts.join(' · ');
  const hasDetail = hasPros || hasCons || showSources;
  return (
    <div className={`qb-opt${selected ? ' sel' : ''}${expanded ? ' expanded' : ''}`}>
      <div className="qb-opt-top">
        <button type="button" className="qb-opt-pick" aria-pressed={selected} onClick={onClick}>
          <span className="qb-n">{n}</span>
          <span>
            <span className="qb-title">{option.title}</span>
            {recommended && <span className="qb-rec">Recommended</span>}
          </span>
          <span className="qb-sum">{option.summary}</span>
        </button>
        {hasDetail && (
          <button
            type="button"
            className="qb-more"
            aria-label={`Show pros, cons and sources for ${option.title}`}
            aria-expanded={expanded}
            aria-controls={expanded ? detailId : undefined}
            onClick={onToggle}
          >
            {more}
          </button>
        )}
      </div>
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
              {sources.map((s, i) =>
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
              {researchOff && 'model knowledge'}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
