export function formatVersionWithLocalTime(version: string): string {
  const match = version.match(/^(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2})(.*)/);
  if (!match) return version;

  const [, date, time, rest] = match;
  const utc = new Date(`${date}T${time}:00Z`);
  if (isNaN(utc.getTime())) return version;

  const local = utc.toLocaleString(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });

  return `${local}${rest}`;
}
