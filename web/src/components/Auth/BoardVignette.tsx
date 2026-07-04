import { type ReactNode } from 'react';

/**
 * Decorative mini-kanban for the AuthShell brand panel. Pure illustration:
 * fake cards, one "claimed" chip with a pulsing agent dot, and (with motion
 * allowed) a ghost chip that hops from doing to done every few seconds.
 */
export function BoardVignette() {
  return (
    <div className="mt-14 grid grid-cols-3 gap-3 max-w-[400px]" aria-hidden="true">
      <Column label="todo" count={4}>
        <Chip dot="--red" w1="62%" w2="38%" />
        <Chip dot="--yellow" w1="48%" w2="30%" />
        <Chip dot="--blue" w1="70%" w2="44%" />
      </Column>
      <Column label="doing" count={2}>
        <AgentChip />
        <div className="auth-vg-ghost">
          <Chip dot="--yellow" w1="58%" w2="40%" />
        </div>
      </Column>
      <Column label="done" count={7}>
        <Chip dot="--green" w1="54%" w2="34%" />
        <Chip dot="--green" w1="66%" w2="42%" />
      </Column>
    </div>
  );
}

function Column({ label, count, children }: { label: string; count: number; children: ReactNode }) {
  return (
    <div>
      <div className="auth-vg-label">
        {label} <em>{count}</em>
      </div>
      <div className="relative flex flex-col gap-2">{children}</div>
    </div>
  );
}

function Chip({ dot, w1, w2 }: { dot: string; w1: string; w2: string }) {
  return (
    <div className="auth-vg-chip">
      <span className="flex items-center gap-1.5">
        <span className="auth-vg-dot" style={{ background: `var(${dot})` }} />
        <span className="auth-vg-bar" style={{ width: w1 }} />
      </span>
      <span className="flex pl-2.5">
        <span className="auth-vg-bar opacity-60" style={{ width: w2 }} />
      </span>
    </div>
  );
}

function AgentChip() {
  return (
    <div className="auth-vg-chip auth-vg-chip--agent">
      <span className="flex items-center gap-1.5">
        <span className="auth-vg-dot" style={{ background: 'var(--orange)' }} />
        <span className="auth-vg-id">CTX-214</span>
        <span className="auth-agentdot" />
      </span>
      <span className="flex pl-2.5">
        <span className="auth-vg-bar opacity-60" style={{ width: '64%' }} />
      </span>
    </div>
  );
}
