const PREFIX: Record<string, string> = {
  github: 'New issue from GitHub',
  jira: 'Imported from Jira',
};

/** Toast text for a card.created event from an importer, or null for any other card. */
export function sourceToastMessage(data: Record<string, unknown> | undefined, cardId: string): string | null {
  const system = data?.source_system;
  if (typeof system !== 'string' || !PREFIX[system]) return null;
  const title = typeof data?.title === 'string' && data.title ? data.title : cardId;
  return `${PREFIX[system]}: ${title}`;
}
