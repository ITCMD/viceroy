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
  set_category_id: number | null;
  set_merchant: string;
  add_tag_id: number | null;
  set_hidden: boolean;
};

export const rulesQuery = queryOptions({
  queryKey: ["rules"],
  queryFn: () => api.get<{ rules: Rule[] }>("/rules"),
});

export const fieldLabels = { merchant: "Merchant", description: "Original statement" } as const;
export const opLabels = { contains: "contains", equals: "is exactly", starts_with: "starts with" } as const;
