import type { ReactNode } from 'react';

/** A dashed cloud with the revision's numbered triangle in the margin, like a drawing revision mark. */
export function RevisionCloud({ version, children }: { version: number; children: ReactNode }) {
  return (
    <div className="bp-cloud">
      <RevisionMark version={version} />
      {children}
    </div>
  );
}

/** The cloud's triangle and announcement, for a cloud its own element draws, such as a table row. */
export function RevisionMark({ version }: { version: number }) {
  return (
    <>
      <svg className="bp-cloud-tri" viewBox="0 0 16 14" aria-hidden="true">
        <path d="M8 1 15 13H1z" />
        <text x="8" y="11.5" textAnchor="middle">
          {version}
        </text>
      </svg>
      <span className="sr-only">Changed in rev {version}.</span>
    </>
  );
}
