// Mirrors the server's jira.IsIssueKey; keys are compared upper-cased, as
// jira.ParseIssueRef does.
const ISSUE_KEY = /^[A-Z][A-Z0-9_]+-[1-9][0-9]*$/;

function asKey(s: string | null): string | null {
  const key = s?.toUpperCase() ?? '';
  return ISSUE_KEY.test(key) ? key : null;
}

function safeDecode(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch {
    return '';
  }
}

/**
 * Returns the issue key in a pasted key or Jira URL, or null. URLs must be
 * https on the configured site's host: /browse/<KEY>, or any page carrying
 * ?selectedIssue=<KEY>. The same rules as jira.ParseIssueRef.
 */
export function parseJiraRef(input: string, siteURL: string): string | null {
  const s = input.trim();
  const bare = asKey(s);
  if (bare) return bare;

  let url: URL;
  let site: URL;
  try {
    url = new URL(s);
    site = new URL(siteURL);
  } catch {
    return null;
  }
  if (url.protocol !== 'https:' || url.host !== site.host) return null;

  const browse = url.pathname.match(/^\/browse\/([^/]+)\/?$/);
  return (browse && asKey(safeDecode(browse[1]))) || asKey(url.searchParams.get('selectedIssue'));
}
