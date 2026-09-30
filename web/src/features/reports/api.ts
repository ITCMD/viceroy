import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type Interval = "month" | "quarter" | "year";
export type By = "category" | "group" | "merchant";

export type Bucket = { key: string; start: string; end: string };
export type Line = { key: string; id: number; name: string; icon: string; total: number; values: number[] };
export type Side = { total: number; values: number[]; lines: Line[] };
export type Report = { from: string; to: string; interval: Interval; by: By; buckets: Bucket[]; income: Side; spending: Side };

export type RangeKey = "1m" | "3m" | "6m" | "12m" | "ytd" | "all";

export const rangeItems: { value: RangeKey; label: string }[] = [
  { value: "1m", label: "1M" },
  { value: "3m", label: "3M" },
  { value: "6m", label: "6M" },
  { value: "12m", label: "12M" },
  { value: "ytd", label: "YTD" },
  { value: "all", label: "All" },
];

const pad = (n: number) => String(n).padStart(2, "0");

/** Start date for a range preset, in whole months ending with the current one. */
export function rangeFrom(r: RangeKey, now = new Date()): string {
  if (r === "all") return "all";
  if (r === "ytd") return `${now.getFullYear()}-01-01`;
  const back = { "1m": 0, "3m": 2, "6m": 5, "12m": 11 }[r];
  const d = new Date(now.getFullYear(), now.getMonth() - back, 1);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-01`;
}

export const reportQuery = (from: string, interval: Interval, by: By) =>
  queryOptions({
    queryKey: ["reports", from, interval, by],
    queryFn: () => api.get<Report>(`/reports?from=${from}&interval=${interval}&by=${by}`),
    placeholderData: keepPreviousData,
  });

export type SpendingPace = { month: string; prev_month: string; today: string; days_in_month: number; this: number[]; last: number[] };

export const spendingPaceQuery = queryOptions({
  queryKey: ["reports", "pace"],
  queryFn: () => api.get<SpendingPace>("/reports/spending-pace"),
});

const fmt = (d: string, o: Intl.DateTimeFormatOptions) => new Date(d + "T00:00:00").toLocaleDateString("en-US", o);

/** Axis label for a bucket: "Sep" (or "Sep 25" across years), "Q3 2026", "2026". */
export function bucketLabel(b: Bucket, interval: Interval, multiYear: boolean) {
  if (interval === "year") return b.key;
  if (interval === "quarter") return b.key.replace(/^(\d{4})-(Q\d)$/, "$2 $1");
  return fmt(b.start, multiYear ? { month: "short", year: "2-digit" } : { month: "short" });
}

export const TOP_LINES = 7;
