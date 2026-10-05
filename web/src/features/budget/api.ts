import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type View = "month" | "week" | "paycheck";

export type Chunk = (
  | { kind: "even" }
  | { kind: "day"; day: number }
  | { kind: "week"; week: number }
  | { kind: "every_n_weeks"; weeks: number; anchor: string }
) & { /** Left out of pacing: no "expected by today" marker or pacing alerts. */ no_pacing?: boolean };

export type BudgetLine = {
  id: number;
  name: string;
  icon: string;
  budget: number;
  actual: number;
  expected: number;
  month_budget: number;
  chunk: Chunk;
  /** Hidden from the budget (still counted in totals). */
  hidden: boolean;
  /** Recurring charges still to come in the upcoming window (a week, or until payday). */
  upcoming: number;
  /** Non-monthly: unspent budget (or overspending, negative) carried in from earlier months; `budget` includes it. */
  rollover: number;
  /** Debt Repayment: one sub-line per debt account (account_id set), then "Other debt payments" (other). */
  lines?: BudgetLine[];
  account_id?: number;
  other?: boolean;
};

export type GroupKind = "income" | "fixed" | "flexible" | "non_monthly" | "goals";

export type BudgetGroup = { id: number; name: string; kind: GroupKind; budget: number; actual: number; rollover: number; lines: BudgetLine[] };

export type PaySchedule = { kind: "weekly" | "biweekly" | "semimonthly" | "monthly"; anchor?: string; days?: number[] };

export type BudgetSettings = { forward_default: boolean; week_start: number; pay_schedule: PaySchedule; upcoming_window: "week" | "paycheck";
  /** What a debt account's Debt Repayment line counts: payments less new charges, or payments. */
  debt_actual: "net" | "paid";
};

export type Budget = {
  view: View;
  start: string;
  end: string;
  prev: string;
  next: string;
  month: string;
  today: string;
  settings: BudgetSettings;
  groups: BudgetGroup[];
  summary: {
    income_budget: number;
    income_actual: number;
    expense_budget: number;
    expense_actual: number;
    goals_budget: number;
    goals_actual: number;
    left_to_budget: number;
    left_actual: number;
    unbudgeted_spend_count: number;
  };
};

export type DebtLineInfo = {
  balance: number;
  apr_bps: number;
  apr_source: "user" | "missing";
  min_payment: number;
  min_payment_source: "user" | "bill" | "missing";
  /** Every debt has its terms, so the payoff plan exists. */
  ready: boolean;
  strategy: "snowball" | "avalanche";
  extra: number;
  /** What the saved plan pays on this debt in the month; null before this month or without a plan. */
  plan_payment: number | null;
  plan_months: number;
  mode: "net" | "paid";
};

export type History = {
  month: string;
  month_budget: number;
  /** paid/charged: debt account lines only. */
  history: { month: string; budget: number; actual: number; paid?: number; charged?: number }[];
  last_month: number;
  average: number;
  chunk: Chunk;
  debt?: DebtLineInfo;
};

/**
 * A budget line is a category, a goal, or a debt account under Debt Repayment. `other` is the
 * Debt Repayment category's own "Other debt payments" line.
 */
export type Target = { kind: "category" | "goal" | "account"; id: number; other?: boolean; /** account: the Debt Repayment category (its timing paces the line). */ categoryId?: number };

export const budgetQuery = (view: View, date: string) =>
  queryOptions({
    queryKey: ["budget", view, date],
    queryFn: () => api.get<Budget>(`/budget?view=${view}${date ? `&date=${date}` : ""}`),
    placeholderData: (prev) => prev,
  });

export const historyQuery = (t: Target, month: string) =>
  queryOptions({
    queryKey: ["budget", "history", t.kind, t.id, !!t.other, month],
    queryFn: () => api.get<History>(`/budget/history?${t.kind}_id=${t.id}&month=${month}${t.other ? "&part=other" : ""}`),
  });

/** Mutations that change budget numbers refresh every budget view and the goals list. */
export function useBudgetMutation<TVars, TRes = unknown>(fn: (v: TVars) => Promise<TRes>, onSuccess?: (r: TRes) => void) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess,
    onSettled: () => Promise.all(["budget", "goals", "settings"].map((k) => qc.invalidateQueries({ queryKey: [k] }))),
  });
}

/** Remaining for a line: what's left to spend, or for income what's still to come. */
export const remaining = (l: { budget: number; actual: number }) => l.budget - l.actual;

const fmt = (d: string, o: Intl.DateTimeFormatOptions) => new Date(d + "T00:00:00").toLocaleDateString("en-US", o);

/** "September 2026", or "Sep 27 – Oct 3" for weeks and paychecks (year added when it differs from today). */
export function periodLabel(b: Pick<Budget, "view" | "start" | "end" | "today">) {
  if (b.view === "month") return fmt(b.start, { month: "long", year: "numeric" });
  const thisYear = b.start.slice(0, 4) === b.today.slice(0, 4) && b.end.slice(0, 4) === b.today.slice(0, 4);
  const o: Intl.DateTimeFormatOptions = thisYear ? { month: "short", day: "numeric" } : { month: "short", day: "numeric", year: "numeric" };
  return `${fmt(b.start, o)} – ${fmt(b.end, o)}`;
}

export const monthLabel = (m: string) => fmt(m + "-01", { month: "long", year: "numeric" });

export const shortMonth = (m: string) => fmt(m + "-01", { month: "short" });

export function chunkLabel(c: Chunk) {
  switch (c.kind) {
    case "day":
      return `On day ${c.day}`;
    case "week":
      return c.week === 4 ? "In the last week" : `In week ${c.week}`;
    case "every_n_weeks":
      return c.weeks === 1 ? "Every week" : `Every ${c.weeks} weeks`;
    default:
      return "";
  }
}

export const weekdays = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

/** Cents > the editable dollars string ("250", "12.50"). */
export function centsToInput(c: number) {
  return c % 100 === 0 ? String(c / 100) : (c / 100).toFixed(2);
}
