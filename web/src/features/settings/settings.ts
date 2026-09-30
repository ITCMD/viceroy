import { queryOptions } from "@tanstack/react-query";
import type { Logo } from "@/components/AppLogo";
import type { BudgetSettings } from "@/features/budget/api";
import { api } from "@/lib/api";

export type Settings = { paper_cash_enabled: boolean; logo: Logo; budget: BudgetSettings };

export const settingsQuery = queryOptions({ queryKey: ["settings"], queryFn: () => api.get<Settings>("/settings") });
