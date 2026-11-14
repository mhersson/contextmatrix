import { Fragment } from 'react';
import { useLLMChannel } from '../../hooks/useLLMChannel';
import { useSentinel } from '../../hooks/useSentinel';
import { subLineItems, subLineText } from '../../lib/llmChannel';
import { LLMChannelChip } from './LLMChannelChip';
import { SentinelChip } from './SentinelChip';

/** The brand header's chips (LLM channel, Sentinel) with the channel notice under them. */
export function StatusRow() {
  const channel = useLLMChannel();
  const { info } = useSentinel();
  const { view } = channel;
  const hasLLM = Boolean(view?.claude_code);
  if (!hasLLM && !info?.status.enabled) return null;

  const items = view?.claude_code ? subLineItems(view) : [];
  return (
    <>
      <div className="sb-status-row">
        <LLMChannelChip state={channel} />
        <SentinelChip />
      </div>
      {items.length > 0 && (
        <p className="llm-sub" title={items.map((item) => item.title ?? subLineText(item)).join(' · ')}>
          {items.map((item, i) => (
            <Fragment key={i}>
              {i > 0 && ' · '}
              {item.count !== undefined && <><b>{item.count}</b> </>}
              {item.text}
            </Fragment>
          ))}
        </p>
      )}
    </>
  );
}
