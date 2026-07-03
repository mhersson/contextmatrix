/**
 * UI-only display labels for GitHub credential kinds. The `kind` value on
 * disk and on the wire stays unchanged ("pat", "app") — this map only
 * affects rendering. Single source of truth for every site that renders a
 * credential kind (Admin credentials table, the create/rotate modal's kind
 * selector); don't duplicate the mapping locally.
 */
export const CREDENTIAL_KIND_LABEL: Record<'pat' | 'app', string> = {
  pat: 'PAT',
  app: 'GitHub App',
};
