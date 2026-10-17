/** "git@host:owner/name.git" or "https://host/owner/name" -> "owner/name"; anything else as given. */
export function repoLabel(repo: string | undefined): string {
  const value = repo?.trim() ?? '';
  const match = /^(?:[a-z+]+:\/\/[^/]+\/|[^@\s/]+@[^:\s/]+:)(.+?)(?:\.git)?\/?$/i.exec(value);
  return match ? match[1] : value;
}
