import { Link } from 'react-router';
import { spendLine, stripText, windowLines } from '../../lib/sentinel';
import type { SentinelStatus } from '../../types';

/** Popover body: state, spend or windows by channel, the day's heals and, for admins, the settings link. */
export function SentinelDetails({ status, isAdmin }: { status: SentinelStatus; isAdmin: boolean }) {
  const lines = status.mode === 'claude_code' ? windowLines(status) : status.mode === 'endpoint' ? [spendLine(status)] : [];
  return (
    <>
      <p className="llm-line">
        <b>Sentinel</b> {stripText(status)}
      </p>
      {lines.map((line) => (
        <p key={line} className="llm-line">
          {line}
        </p>
      ))}
      <p className="llm-line">
        <b>{status.healed_today}</b> {status.healed_today === 1 ? 'card' : 'cards'} healed today
      </p>
      {isAdmin && (
        <Link to="/admin/sentinel" className="llm-line">
          Sentinel settings
        </Link>
      )}
    </>
  );
}
