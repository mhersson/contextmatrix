import { componentLabel, formatBuilt, imageMismatch, shortSha, versionLabel } from '../../lib/about';
import type { AboutComponent } from '../../types';

const MISMATCH_TITLE = 'The default worker image was built from a different commit than this server.';
const muted = { color: 'var(--blueprint-quiet-strong)' };
// The dialog sits on --bg2, so row rules take the next step up to stay visible.
const line = 'border-b border-[var(--bg3)]';

// Offers a wrap point only at a camel-case boundary, so "ContextMatrix" splits as "Context / Matrix".
function WrapLabel({ text }: { text: string }) {
  const parts = text.split(/(?<=[a-z])(?=[A-Z])/);
  return (
    <>
      {parts.map((part, i) => (
        <span key={i}>
          {i > 0 && <wbr />}
          {part}
        </span>
      ))}
    </>
  );
}

function Image({ c }: { c: AboutComponent }) {
  if (c.name !== 'agent' && c.name !== 'chat') return null;
  if (!c.worker_image_revision) return null;
  return (
    <span className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
      <span className="font-mono">{shortSha(c.worker_image_revision)}</span>
      {imageMismatch(c) && (
        <span
          title={MISMATCH_TITLE}
          className="whitespace-nowrap rounded px-1 text-[0.65rem] ring-1 ring-[var(--orange)]"
          style={{ color: 'var(--fg)', backgroundColor: 'var(--bg-orange)' }}
        >
          differs
        </span>
      )}
    </span>
  );
}

export function RunningTable({ components }: { components: AboutComponent[] }) {
  return (
    <table className="w-full table-fixed border-collapse text-sm">
      <thead>
        <tr className="text-left text-xs" style={muted}>
          {['Component', 'Version', 'Image', 'Built', 'Status'].map((h) => (
            <th key={h} scope="col" className={`w-1/5 pb-1 text-left font-normal ${line}`}>
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
              <td className={`py-1.5 pr-2 [overflow-wrap:anywhere] ${line}`}>
                <WrapLabel text={componentLabel(c.name)} />
              </td>
              <td className={`py-1.5 pr-2 font-mono [overflow-wrap:anywhere] ${line}`}>{versionLabel(c)}</td>
              <td className={`py-1.5 pr-2 ${line}`} style={muted}>
                <Image c={c} />
              </td>
              <td className={`py-1.5 pr-2 [overflow-wrap:anywhere] ${line}`} style={muted}>
                {formatBuilt(c.build?.build_time)}
              </td>
              <td className={`py-1.5 [overflow-wrap:anywhere] ${line}`} style={{ color: up ? 'var(--blueprint-ink-red)' : 'var(--blueprint-ink-green)' }}>
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
