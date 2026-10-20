import { componentLabel, formatBuilt, imageMismatch, shortSha, versionLabel } from '../../lib/about';
import type { AboutComponent } from '../../types';

const MISMATCH_TITLE = 'The default worker image was built from a different commit than this server.';
const muted = { color: 'var(--blueprint-quiet-strong)' };

function Image({ c }: { c: AboutComponent }) {
  if (c.name !== 'agent' && c.name !== 'chat') return null;
  if (!c.worker_image_revision) return null;
  return (
    <div className="mt-0.5 flex items-center gap-1.5 text-xs" style={muted}>
      <span className="font-mono">image {shortSha(c.worker_image_revision)}</span>
      {imageMismatch(c) && (
        <span
          title={MISMATCH_TITLE}
          className="rounded px-1 text-[0.65rem] ring-1 ring-[var(--orange)]"
          style={{ color: 'var(--fg)', backgroundColor: 'var(--bg-orange)' }}
        >
          differs
        </span>
      )}
    </div>
  );
}

export function RunningTable({ components }: { components: AboutComponent[] }) {
  return (
    <table className="w-full table-fixed border-collapse text-sm">
      <thead>
        <tr className="text-left text-xs" style={muted}>
          {['Component', 'Version', 'Built', 'Status'].map((h) => (
            <th key={h} scope="col" className="w-1/4 pb-1 text-left font-normal">
              {h}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {components.map((c, i) => {
          const up = c.status === 'unreachable';
          return (
            <tr key={`${c.name}-${i}`} className="align-top" style={{ color: 'var(--fg)' }}>
              <td className="py-1.5 pr-2 [overflow-wrap:anywhere]">
                {componentLabel(c.name)}
                <Image c={c} />
              </td>
              <td className="py-1.5 pr-2 font-mono [overflow-wrap:anywhere]">{versionLabel(c)}</td>
              <td className="py-1.5 pr-2 [overflow-wrap:anywhere]" style={muted}>
                {formatBuilt(c.build?.build_time)}
              </td>
              <td className="py-1.5 [overflow-wrap:anywhere]" style={{ color: up ? 'var(--blueprint-ink-red)' : 'var(--blueprint-ink-green)' }}>
                <span aria-hidden="true" style={{ color: up ? 'var(--red)' : 'var(--green)' }}>
                  ●
                </span>{' '}
                {c.status}
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
