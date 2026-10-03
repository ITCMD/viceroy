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
const iso = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

/** Start date for a range preset, in whole months ending with the current one. */
export function rangeFrom(r: RangeKey, now = new Date()): string {
  return rangeWindow(r, 0, now).from;
}

export type RangeWindow = { from: string; to: string; label: string };

const monthsIn: Record<string, number> = { "1m": 1, "3m": 3, "6m": 6, "12m": 12 };

/**
 * The dates a range preset covers, `offset` windows back from the current one (0 = ending
 * now): 3M with offset 1 is the three months before the last three. YTD steps by whole
 * years. `to` is empty while the window includes today (the server ends at today).
 */
export function rangeWindow(r: RangeKey, offset: number, now = new Date()): RangeWindow {
  if (r === "all") return { from: "all", to: "", label: "All time" };
  if (r === "ytd") {
    const y = now.getFullYear() - offset;
    return { from: `${y}-01-01`, to: offset ? `${y}-12-31` : "", label: offset ? String(y) : `${y} to date` };
  }
  const n = monthsIn[r];
  const end = new Date(now.getFullYear(), now.getMonth() - offset * n, 1);
  const start = new Date(end.getFullYear(), end.getMonth() - (n - 1), 1);
  const last = new Date(end.getFullYear(), end.getMonth() + 1, 0);
  const mon = (d: Date, year: boolean) => d.toLocaleDateString("en-US", year ? { month: "short", year: "numeric" } : { month: "short" });
  const label =
    n === 1
      ? end.toLocaleDateString("en-US", { month: "long", year: "numeric" })
      : start.getFullYear() === end.getFullYear()
        ? `${mon(start, false)} – ${mon(end, true)}`
        : `${mon(start, true)} – ${mon(end, true)}`;
  return { from: iso(start), to: offset ? iso(last) : "", label };
}

const rangeParams = (from: string, to: string) => `from=${from}${to ? `&to=${to}` : ""}`;

export const reportQuery = (from: string, interval: Interval, by: By, to = "") =>
  queryOptions({
    queryKey: ["reports", from, to, interval, by],
    queryFn: () => api.get<Report>(`/reports?${rangeParams(from, to)}&interval=${interval}&by=${by}`),
    placeholderData: keepPreviousData,
  });

/** A box in the spending tree / cash flow diagram (see internal/reports/tree.go). */
export type TreeNode = {
  key: string;
  id: number;
  name: string;
  icon: string;
  kind: "income" | "spending" | "group" | "category" | "goal" | "uncategorized" | "contributions" | "merchant";
  group: string;
  total: number;
  count: number;
  children?: TreeNode[];
};
export type ReportTree = { from: string; to: string; income: TreeNode; spending: TreeNode };

export const treeQuery = (from: string, to = "") =>
  queryOptions({
    queryKey: ["reports", "tree", from, to],
    queryFn: () => api.get<ReportTree>(`/reports/tree?${rangeParams(from, to)}`),
    placeholderData: keepPreviousData,
  });

export type Debt = {
  account_id: number;
  name: string;
  type: string;
  institution_name: string;
  color: string;
  logo_url: string | null;
  balance: number;
  apr_bps: number;
  apr_source: "user" | "missing";
  /** 0% intro rate through this date (YYYY-MM-DD), then apr_bps. */
  promo_until?: string;
  min_payment: number;
  min_payment_source: "user" | "bill" | "missing";
  monthly_interest: number;
  interest_paid_12m: number;
};
export type DebtPlan = {
  strategy: "minimum" | "snowball" | "avalanche";
  months: number;
  interest: number;
  payment: number;
  never: boolean;
  balances: number[];
  /** months: plan month it's paid off in (1 = this month, 0 = never). payments: paid each plan month, from this month. */
  debts: { id: number; months: number; interest: number; order: number; payments: number[] }[];
};
export type DebtReport = {
  debts: Debt[];
  total: number;
  monthly_interest: number;
  interest_paid_12m: number;
  history: { date: string; total: number; accounts: Record<string, number> }[];
  start: string;
  extra: number;
  /** The saved plan's strategy; extra defaults to the saved plan's too. */
  strategy: "snowball" | "avalanche";
  /** Every debt has an APR and a minimum; plans is empty until then. */
  ready: boolean;
  plans: Partial<Record<DebtPlan["strategy"], DebtPlan>>;
};

/** The debt report; extraCents null = the saved plan's extra. */
export const debtQuery = (extraCents: number | null) =>
  queryOptions({
    queryKey: ["reports", "debt", extraCents],
    queryFn: () => api.get<DebtReport>(`/reports/debt${extraCents === null ? "" : `?extra=${(extraCents / 100).toFixed(2)}`}`),
    placeholderData: keepPreviousData,
  });

/** "Mar 2028" for plan month n (1 = start, this month; start is YYYY-MM). */
export function planMonth(start: string, n: number, o: Intl.DateTimeFormatOptions = { month: "short", year: "numeric" }) {
  const [y, m] = start.split("-").map(Number);
  return new Date(y, m - 2 + n, 1).toLocaleDateString("en-US", o);
}

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
