import { queryOptions } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type Rule = {
  id: number;
  priority: number;
  match_field: "merchant" | "description";
  match_op: "contains" | "equals" | "starts_with";
  match_value: string;
  account_id: number | null;
  amount_min: number | null;
  amount_max: number | null;
  direction: "" | "out" | "in";
  day_min: number | null;
  day_max: number | null;
  set_category_id: number | null;
  set_merchant: string;
  tags: { id: number; name: string }[];
  set_goal_id: number | null;
  set_hidden: boolean;
};

/** A rule being made from a transaction edit: conditions and actions to prefill. */
export type RuleDraft = Partial<Omit<Rule, "id" | "priority" | "tags">> & { tags?: string[] };

export const rulesQuery = queryOptions({
  queryKey: ["rules"],
  queryFn: () => api.get<{ rules: Rule[] }>("/rules"),
});

export const fieldLabels = { merchant: "Merchant", description: "Original statement" } as const;
export const opLabels = { contains: "contains", equals: "is exactly", starts_with: "starts with" } as const;

const dollars = (c: number) => `$${(c / 100).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;

/** Plain-words conditions of a rule, e.g. ["Merchant contains “Shell”", "money out", "$20.00–$80.00", "days 1–7"]. */
export function ruleConditions(r: Pick<Rule, "match_field" | "match_op" | "match_value" | "amount_min" | "amount_max" | "direction" | "day_min" | "day_max">) {
  const out: string[] = [];
  if (r.match_value) out.push(`${fieldLabels[r.match_field]} ${opLabels[r.match_op]} “${r.match_value}”`);
  if (r.direction === "out") out.push("money out");
  if (r.direction === "in") out.push("money in");
  if (r.amount_min !== null && r.amount_max !== null)
    out.push(r.amount_min === r.amount_max ? `exactly ${dollars(r.amount_min)}` : `${dollars(r.amount_min)}–${dollars(r.amount_max)}`);
  else if (r.amount_min !== null) out.push(`at least ${dollars(r.amount_min)}`);
  else if (r.amount_max !== null) out.push(`at most ${dollars(r.amount_max)}`);
  if (r.day_min !== null || r.day_max !== null) out.push(`days ${r.day_min ?? 1}–${r.day_max ?? 31} of the month`);
  return out;
}
