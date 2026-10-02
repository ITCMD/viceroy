const usd = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" });
const usdWhole = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 0 });

/** Formats integer cents. All money in Viceroy is int cents end to end. */
export function formatMoney(cents: number, opts: { whole?: boolean } = {}) {
  return (opts.whole ? usdWhole : usd).format(cents / 100);
}

/** "just now", "5m ago", "3h ago", "2d ago", or a date for older unix-second timestamps. */
export function timeAgo(unix: number | null | undefined) {
  if (!unix) return "never";
  const s = Math.max(0, Date.now() / 1000 - unix);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  if (s < 7 * 86400) return `${Math.floor(s / 86400)}d ago`;
  return new Date(unix * 1000).toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

/** "in 5m", "in 3h", "in 2d" for a future unix-second timestamp. */
export function timeUntil(unix: number) {
  const s = Math.max(0, unix - Date.now() / 1000);
  if (s < 3600) return `in ${Math.max(1, Math.round(s / 60))}m`;
  if (s < 86400) return `in ${Math.round(s / 3600)}h`;
  return `in ${Math.round(s / 86400)}d`;
}
