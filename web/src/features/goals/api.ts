import { queryOptions } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type Goal = {
  id: number;
  name: string;
  icon: string;
  target_cents: number;
  target_date: string | null;
  starting_cents: number;
  contributed_cents: number;
  balance_cents: number;
  archived: boolean;
};

export const goalsQuery = queryOptions({
  queryKey: ["goals"],
  queryFn: () => api.get<{ goals: Goal[] }>("/goals"),
});
