import { chipTint } from '../../lib/chip';
import { CREDENTIAL_KIND_LABEL } from '../../lib/credentialLabels';
import type { CredentialInfo } from '../../types';

interface CredentialTableRowProps {
  credential: CredentialInfo;
  onRotate: () => void;
  onToggleDisabled: () => void;
  onDelete: () => void;
}

function formatLastUsed(ts?: string): string {
  return ts ? new Date(ts).toLocaleString() : 'Never';
}

/**
 * One row of the Credentials table. Purely presentational — AdminCredentialsPage
 * decides whether a toggle needs confirmation and owns the API calls; this
 * component only renders the row and reports clicks. Never receives or
 * renders a secret value — CredentialInfo has no secret field.
 */
export function CredentialTableRow({ credential, onRotate, onToggleDisabled, onDelete }: CredentialTableRowProps) {
  return (
    <tr className="border-t" style={{ borderColor: 'var(--bg3)' }}>
      <td className="px-4 py-2 font-mono">{credential.name}</td>
      <td className="px-4 py-2">
        <span className="chip-pill" style={chipTint(credential.kind === 'app' ? 'var(--purple)' : 'var(--aqua)')}>
          {CREDENTIAL_KIND_LABEL[credential.kind]}
        </span>
      </td>
      <td className="px-4 py-2" style={{ color: 'var(--grey1)' }}>
        {credential.host || 'github.com'}
      </td>
      <td className="px-4 py-2 font-mono" style={{ color: 'var(--grey1)' }}>
        {credential.kind === 'app' ? credential.app_id : '—'}
      </td>
      <td className="px-4 py-2 font-mono" style={{ color: 'var(--grey1)' }}>
        {credential.kind === 'app' ? credential.installation_id : '—'}
      </td>
      <td className="px-4 py-2">{credential.created_by}</td>
      <td className="px-4 py-2" style={{ color: 'var(--grey1)' }}>
        {formatLastUsed(credential.last_used_at)}
      </td>
      <td className="px-4 py-2">
        <span className="chip-pill" style={chipTint(credential.disabled ? 'var(--red)' : 'var(--green)')}>
          {credential.disabled ? 'Disabled' : 'Active'}
        </span>
      </td>
      <td className="px-4 py-2 text-right whitespace-nowrap">
        <button
          type="button"
          onClick={onRotate}
          className="text-xs px-2 py-1 rounded mr-1 hover:opacity-80"
          style={{ backgroundColor: 'var(--bg2)', color: 'var(--aqua)' }}
        >
          Rotate
        </button>
        <button
          type="button"
          onClick={onToggleDisabled}
          className="text-xs px-2 py-1 rounded mr-1 hover:opacity-80"
          style={{ backgroundColor: 'var(--bg2)', color: credential.disabled ? 'var(--green)' : 'var(--orange)' }}
        >
          {credential.disabled ? 'Enable' : 'Disable'}
        </button>
        <button
          type="button"
          onClick={onDelete}
          className="text-xs px-2 py-1 rounded hover:opacity-80"
          style={{ backgroundColor: 'var(--bg2)', color: 'var(--red)' }}
        >
          Delete
        </button>
      </td>
    </tr>
  );
}
