import { useId } from 'react';
import type { Card } from '../../../types';
import { gitHubIcon } from '../../icons';
import { isSafeHttpUrl } from '../utils';

interface MetadataSourceProps {
  card: Card;
  editedVetted: boolean;
  onVettedChange: (value: boolean) => void;
}

/**
 * Source section of the Info rail tab. Renders only when the card was
 * imported from an external system (GitHub, Jira, …). The external link
 * is guarded by `isSafeHttpUrl` so stored URLs with unexpected schemes
 * degrade to a plain-text label rather than an unsafe anchor.
 *
 * The `vetted` checkbox is always editable (matches existing behavior);
 * an unvetted card is blocked from agent claims, which is explained by
 * the yellow helper text beneath the checkbox.
 */
export function MetadataSource({ card, editedVetted, onVettedChange }: MetadataSourceProps) {
  const vettedId = useId();
  if (!card.source) return null;

  return (
    <section className="bf-aside-section">
      <h4>Source</h4>
      {card.source.external_url && isSafeHttpUrl(card.source.external_url) ? (
        <a
          href={card.source.external_url}
          target="_blank"
          rel="noopener noreferrer"
          className="bf-source-link"
          title={`Imported from ${card.source.system} · ${card.source.external_id}`}
        >
          {card.source.system === 'github' ? (
            <>
              {gitHubIcon}
              <span>#{card.source.external_id}</span>
            </>
          ) : (
            <>
              <span className="font-mono">{card.source.system}</span>
              <span>·</span>
              <span>{card.source.external_id}</span>
            </>
          )}
        </a>
      ) : (
        <div className="font-mono" style={{ color: 'var(--grey1)', fontSize: '11px' }}>
          {card.source.system} · {card.source.external_id}
        </div>
      )}
      <label htmlFor={vettedId} className="flex items-center gap-2 cursor-pointer mt-3">
        <input
          id={vettedId}
          type="checkbox"
          checked={editedVetted}
          onChange={(e) => onVettedChange(e.target.checked)}
          className="rounded border-[var(--bg3)] bg-[var(--bg2)] accent-[var(--green)]"
        />
        <span className="text-sm text-[var(--fg)]">Content vetted</span>
      </label>
      {!editedVetted && (
        <p className="mt-1 text-xs text-[var(--yellow)]">
          Agents cannot claim this card until it is marked as vetted.
        </p>
      )}
    </section>
  );
}
