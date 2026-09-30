import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type Cadence = "weekly" | "biweekly" | "semimonthly" | "monthly" | "quarterly" | "yearly";

export type RecurringSeries = {
  key: string;
  name: string;
  merchant_id: number;
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
  dismissed: boolean;
};

export const recurringQuery = queryOptions({
  queryKey: ["recurring"],
  queryFn: () => api.get<{ series: RecurringSeries[]; today: string }>("/recurring"),
});

export function useDismissRecurring() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { key: string; dismissed: boolean }) => api.put("/recurring/dismissed", v),
    onSettled: () => qc.invalidateQueries({ queryKey: ["recurring"] }),
  });
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
