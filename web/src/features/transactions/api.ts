import { infiniteQueryOptions, queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type Tag = { id: number; name: string; color: string };

export type Transaction = {
  id: number;
  account_id: number;
  account_name: string;
  account_mask: string;
  date: string;
  amount_cents: number;
  description: string;
  merchant: string;
  merchant_id: number | null;
  category_id: number | null;
  category_name: string;
  category_icon: string;
  category_source: "" | "rule" | "history" | "ai" | "user" | "linked";
  notes: string;
  hidden: boolean;
  needs_review: boolean;
  pending: boolean;
  provisional: boolean;
  source: "manual" | "simplefin" | "email" | "import";
  has_linked: boolean;
  linked_txn_id: number | null;
  goal_id: number | null;
  tags: Tag[];
};

/** Compact row used by similar-transaction and link-candidate lists. */
export type TxnSummary = {
  id: number;
  date: string;
  amount_cents: number;
  description: string;
  merchant_name?: string;
  account_name?: string;
  category_name?: string;
};

export type Category = { id: number; name: string; icon: string };
export type CategoryGroup = { id: number; name: string; kind: string; categories: Category[] };

export type TxnFilters = {
  account?: number;
  q?: string;
  view?: "all" | "review" | "uncategorized";
  hidden?: boolean;
};

export const sourceLabels: Record<string, string> = {
  rule: "Categorized by a rule",
  history: "Categorized from past transactions",
  ai: "Suggested by AI",
  user: "",
  linked: "From the linked pending entry",
};

function filterParams(f: TxnFilters, cursor: string) {
  const p = new URLSearchParams();
  if (f.account) p.set("account", String(f.account));
  if (f.q) p.set("q", f.q);
  if (f.view === "review") p.set("review", "1");
  if (f.view === "uncategorized") p.set("uncategorized", "1");
  if (f.hidden) p.set("hidden", "1");
  if (cursor) p.set("cursor", cursor);
  return p.toString();
}

export const transactionsQuery = (f: TxnFilters) =>
  infiniteQueryOptions({
    queryKey: ["transactions", "list", f],
    queryFn: ({ pageParam }) =>
      api.get<{ transactions: Transaction[]; next_cursor: string }>(`/transactions?${filterParams(f, pageParam)}`),
    initialPageParam: "",
    getNextPageParam: (last) => last.next_cursor || undefined,
  });

export const transactionQuery = (id: number) =>
  queryOptions({
    queryKey: ["transactions", "detail", id],
    queryFn: () => api.get<{ transaction: Transaction; linked: Transaction[]; email: TxnEmail | null }>(`/transactions/${id}`),
  });

export const similarQuery = (id: number) =>
  queryOptions({
    queryKey: ["transactions", "similar", id],
    queryFn: () => api.get<{ transactions: TxnSummary[] }>(`/transactions/${id}/similar`),
  });

export const linkCandidatesQuery = (id: number) =>
  queryOptions({
    queryKey: ["transactions", "link-candidates", id],
    queryFn: () => api.get<{ transactions: TxnSummary[] }>(`/transactions/${id}/link-candidates`),
  });

export const categoriesQuery = queryOptions({
  queryKey: ["categories"],
  queryFn: () => api.get<{ groups: CategoryGroup[] }>("/categories"),
  staleTime: 60_000,
});

export const tagsQuery = queryOptions({
  queryKey: ["tags"],
  queryFn: () => api.get<{ tags: Tag[] }>("/tags"),
});

/** Mutation that refreshes every transaction list and detail when it settles. */
export function useTxnMutation<TVars, TRes = unknown>(fn: (v: TVars) => Promise<TRes>, onSuccess?: (r: TRes) => void) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess,
    onSettled: () =>
      Promise.all(["transactions", "tags", "budget", "goals", "reports", "recurring"].map((k) => qc.invalidateQueries({ queryKey: [k] }))),
  });
}

/** "Today", "Yesterday", or "Wednesday, September 10" (adds the year when it isn't this year). */
export function dayLabel(date: string) {
  const d = new Date(date + "T00:00:00");
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  const diff = Math.round((today.getTime() - d.getTime()) / 86_400_000);
  if (diff === 0) return "Today";
  if (diff === 1) return "Yesterday";
  return d.toLocaleDateString("en-US", {
    weekday: "long",
    month: "long",
    day: "numeric",
    ...(d.getFullYear() !== today.getFullYear() ? { year: "numeric" } : {}),
  });
}

export function shortDate(date: string) {
  return new Date(date + "T00:00:00").toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" });
}

export function todayISO() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/** The alert email an email-sourced transaction came from. */
export type TxnEmail = { id: number; from_addr: string; from_name: string; subject: string; received_at: number };

/** Badge text for a pending row: email alerts and manual pending entries are stand-ins. */
export function pendingLabel(t: { source: string; provisional: boolean }) {
  if (t.source === "email") return "Email alert";
  return t.provisional ? "Pending entry" : "Pending";
}
