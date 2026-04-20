export function formatVersionWithLocalTime(version: string): string {
  const match = version.match(/^(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2})(.*)/);
  if (!match) return version;

  const [, date, time, rest] = match;
  const utc = new Date(`${date}T${time}:00Z`);
  if (isNaN(utc.getTime())) return version;

  const y = utc.getFullYear();
  const m = String(utc.getMonth() + 1).padStart(2, '0');
  const d = String(utc.getDate()).padStart(2, '0');
  const hh = String(utc.getHours()).padStart(2, '0');
  const mm = String(utc.getMinutes()).padStart(2, '0');

  return `${y}-${m}-${d} ${hh}:${mm}${rest}`;
}
