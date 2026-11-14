import type { ReactNode } from 'react';
import { Link } from 'react-router';
import type { SentinelFindingView } from '../../../../types';
import { findingLabel, findingOutcome, stateWord } from '../../../../lib/sentinel';
import { CaretHeader, Empty, ExpandRow, ExternalLink, Heading, Pill, Table } from './SentinelParts';

function Outcome({ finding, project, prURL }: { finding: SentinelFindingView; project: string; prURL?: string }) {
  const outcome = findingOutcome(finding, project, prURL);
  if (!outcome) return null;
  let line: ReactNode = outcome.text;
  if (outcome.href && outcome.external) line = <ExternalLink href={outcome.href}>{outcome.text}</ExternalLink>;
  else if (outcome.href) {
    line = (
      <Link to={outcome.href} className="snt-link">
        {outcome.text}
      </Link>
    );
  }
  return <p className="snt-outcome">{line}</p>;
}

interface FindingProps {
  project: string;
  prURL?: string;
}

function FindingRow({ finding: f, project, prURL }: FindingProps & { finding: SentinelFindingView }) {
  const label = findingLabel(f);
  return (
    <ExpandRow
      label={label}
      span={4}
      cells={
        <>
          <td className="snt-kind">{stateWord(f.kind)}</td>
          <td title={label}>{label}</td>
          <td>{f.state && <Pill state={f.state} />}</td>
        </>
      }
    >
      <p className="snt-text">{f.text}</p>
      {f.evidence && <pre className="snt-evidence">{f.evidence}</pre>}
      <Outcome finding={f} project={project} prURL={prURL} />
    </ExpandRow>
  );
}

function Notes({ notes, project, prURL }: FindingProps & { notes: SentinelFindingView[] }) {
  return (
    <section>
      <Heading count={notes.length}>Notes</Heading>
      <Table>
        <thead>
          <tr>
            <th style={{ width: 104 }}>Note</th>
            <th>Text</th>
          </tr>
        </thead>
        <tbody>
          {notes.map((n) => (
            <tr key={n.id}>
              <td className="snt-wrap">
                <span className="snt-kind">{stateWord(n.kind)}</span>
                {n.kind === 'project_verify' && <span className="snt-pill snt-pill--mark">admin</span>}
              </td>
              <td className="snt-wrap">
                {n.title && <p className="font-medium">{n.title}</p>}
                <p className="snt-text">{n.text}</p>
                {n.evidence && <pre className="snt-evidence">{n.evidence}</pre>}
                <Outcome finding={n} project={project} prURL={prURL} />
              </td>
            </tr>
          ))}
        </tbody>
      </Table>
    </section>
  );
}

/** The card's friction findings, each expanding to its text, evidence and outcome, then its notes. */
export function SentinelFindings({
  friction,
  notes,
  project,
  prURL,
}: FindingProps & { friction: SentinelFindingView[]; notes: SentinelFindingView[] }) {
  return (
    <div className="flex flex-col gap-3">
      {friction.length === 0 ? (
        <Empty>No findings.</Empty>
      ) : (
        <Table>
          <thead>
            <tr>
              <CaretHeader />
              <th style={{ width: 80 }}>Kind</th>
              <th>Finding</th>
              <th style={{ width: 96 }}>Outcome</th>
            </tr>
          </thead>
          <tbody>
            {friction.map((f) => (
              <FindingRow key={f.id} finding={f} project={project} prURL={prURL} />
            ))}
          </tbody>
        </Table>
      )}
      {notes.length > 0 && <Notes notes={notes} project={project} prURL={prURL} />}
    </div>
  );
}
