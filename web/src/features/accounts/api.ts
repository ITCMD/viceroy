import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type AccountStatus = "active" | "review" | "disconnected" | "ignored" | "closed";
export type AccountGroup = "cash" | "credit" | "investments" | "loans" | "other";

export type Account = {
  id: number;
  name: string;
  type: string;
  group: AccountGroup;
  is_liability: boolean;
  institution_name: string;
  institution_status: "ok" | "reauth";
  provider_name: string;
  mask: string;
  balance_cents: number;
  available_cents: number | null;
  balance_at: number | null;
  status: AccountStatus;
  review_candidate_id: number | null;
  include_in_net_worth: boolean;
  hidden: boolean;
  is_manual: boolean;
  /** "paper_cash" for the built-in cash wallet. */
  builtin: "" | "paper_cash";
  connection_id: number | null;
  last_synced_at: number | null;
};

export type Connection = {
  id: number;
  name: string;
  status: "active" | "error" | "revoked";
  last_error: string;
  last_sync_at: number | null;
  next_sync_at: number | null;
  requests_remaining: number;
  institutions: { id: number; name: string; url: string; status: "ok" | "reauth"; last_error: string }[];
  events: { at: number; kind: string; account_id: number | null; message: string }[];
};

export type NetWorthPoint = { date: string; assets: number; liabilities: number; net: number; groups: Partial<Record<AccountGroup, number>> };

export const typeLabels: Record<string, string> = {
  checking: "Checking",
  savings: "Savings",
  cash: "Cash",
  credit_card: "Credit card",
  investment: "Investment",
  loan: "Loan",
  mortgage: "Mortgage",
  other_asset: "Other asset",
  other_liability: "Other liability",
};

export const groupLabels: Record<AccountGroup, string> = {
  cash: "Cash",
  credit: "Credit cards",
  investments: "Investments",
  loans: "Loans",
  other: "Other",
};

export const accountsQuery = queryOptions({
  queryKey: ["accounts"],
  queryFn: () => api.get<{ accounts: Account[]; types: string[] }>("/accounts"),
});

export const connectionsQuery = queryOptions({
  queryKey: ["connections"],
  queryFn: () => api.get<{ connections: Connection[] }>("/connections"),
});

export const netWorthQuery = (days: number) =>
  queryOptions({
    queryKey: ["networth", days],
    queryFn: () => api.get<{ points: NetWorthPoint[] }>(`/networth/history?days=${days}`),
  });

/** Mutation that refreshes everything account-related when it settles. */
export function useAccountsMutation<TVars, TRes = unknown>(fn: (v: TVars) => Promise<TRes>, onSuccess?: (r: TRes) => void) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess,
    onSettled: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: ["accounts"] }),
        qc.invalidateQueries({ queryKey: ["connections"] }),
        qc.invalidateQueries({ queryKey: ["networth"] }),
      ]),
  });
}

/** An account's display line under its name, e.g. "Capital One · ••1111". */
export function accountSubtitle(a: Account) {
  const parts = [a.builtin ? "Cash on hand" : a.is_manual ? "Manual" : a.institution_name || "Linked", a.mask && `••${a.mask}`];
  return parts.filter(Boolean).join(" · ");
}

/** Name plus "••1234" unless the name already shows the last four digits. */
export function accountLabel(a: { name: string; mask: string }) {
  return a.mask && !a.name.includes(a.mask) ? `${a.name} ••${a.mask}` : a.name;
}

/** Accounts a new transaction can be added to. */
export function canAddTo(a: Account) {
  return a.status === "active" || a.status === "disconnected";
}
