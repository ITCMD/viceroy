import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type Cadence = "weekly" | "biweekly" | "semimonthly" | "monthly" | "quarterly" | "yearly";

/** A recurring transaction: tracked (source "tracked", id > 0) or detected from history. */
export type RecurringSeries = {
  id: number;
  source: "tracked" | "detected";
  key: string;
  strong: boolean;
  name: string;
  merchant_id: number;
  match_text: string;
  category_id: number;
  category_name: string;
  category_icon: string;
  account_id: number;
  account_name: string;
  cadence: Cadence;
  amount: number;
  variable: boolean;
  count: number;
  last_date: string;
  next_date: string;
  anchor_date: string;
  day2: number;
  dismissed: boolean;
};

export type Occurrence = {
  key: string;
  item_id: number;
  name: string;
  date: string;
  amount: number;
  status: "paid" | "upcoming" | "due" | "missed";
  txn_id: number;
  category_id: number;
  category_icon: string;
  cadence: Cadence;
  source: "tracked" | "detected";
};

export type RecurringData = {
  today: string;
  next_payday: string;
  tracked: RecurringSeries[];
  suggestions: RecurringSeries[];
  dismissed: RecurringSeries[];
  upcoming: RecurringSeries[];
  before_payday: Occurrence[];
  from: string;
  to: string;
  occurrences: Occurrence[];
};

/** Everything recurring; from/to pick the calendar range (default: this month). */
export const recurringQuery = (from = "", to = "") =>
  queryOptions({
    queryKey: ["recurring", from, to],
    queryFn: () => api.get<RecurringData>(`/recurring${from ? `?from=${from}&to=${to}` : ""}`),
  });

function useRecurringMutation<V>(fn: (v: V) => Promise<unknown>, onSuccess?: () => void) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess,
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ["recurring"] });
      qc.invalidateQueries({ queryKey: ["budget"] });
      qc.invalidateQueries({ queryKey: ["transactions", "detail"] });
    },
  });
}

export function useDismissRecurring() {
  return useRecurringMutation((v: { key: string; dismissed: boolean }) => api.put("/recurring/dismissed", v));
}

export type RecurringInput = Partial<{
  name: string;
  merchant_id: number;
  match_text: string;
  account_id: number;
  category_id: number;
  amount: number;
  amount_varies: boolean;
  cadence: Cadence;
  anchor_date: string;
  day2: number;
  series_key: string;
  transaction_id: number;
}>;

export function useSaveRecurring(id: number | null, onSuccess?: () => void) {
  return useRecurringMutation((v: RecurringInput) => (id ? api.patch(`/recurring/items/${id}`, v) : api.post("/recurring/items", v)), onSuccess);
}

export function useDeleteRecurring(onSuccess?: () => void) {
  return useRecurringMutation((id: number) => api.del(`/recurring/items/${id}`), onSuccess);
}

export const cadenceLabels: Record<Cadence, string> = {
  weekly: "Weekly",
  biweekly: "Every 2 weeks",
  semimonthly: "Twice a month",
  monthly: "Monthly",
  quarterly: "Quarterly",
  yearly: "Yearly",
};

/** "Today", "Tomorrow", "In 5 days", "Oct 12", or "Due Sep 28" when the date has passed. */
export function dueLabel(date: string, today: string) {
  const days = Math.round((Date.parse(date) - Date.parse(today)) / 86_400_000);
  const short = new Date(date + "T00:00:00").toLocaleDateString("en-US", { month: "short", day: "numeric" });
  if (days < 0) return `Due ${short}`;
  if (days === 0) return "Today";
  if (days === 1) return "Tomorrow";
  if (days < 7) return `In ${days} days`;
  return short;
}

/** "12.50" / "$1,200" > cents; NaN when it isn't a number. */
export function inputToCents(s: string) {
  const n = Number(s.replace(/[$,\s]/g, ""));
  return s.trim() === "" ? NaN : Math.round(n * 100);
}
