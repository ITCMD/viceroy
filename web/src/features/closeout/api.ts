import { queryOptions } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type RiskLevel = "low" | "moderate" | "high" | "very_high";

export type RiskDriver = {
  id: number;
  name: string;
  icon: string;
  budget: number;
  actual: number;
  /** What the plan allows through the end of this week (the budget for unpaced categories). */
  planned: number;
  upcoming: number;
  projected: number;
  over: number;
  reason: "over" | "upcoming" | "pace";
};

export type Risk = {
  score: number;
  level: RiskLevel;
  month: string;
  start: string;
  end: string;
  today: string;
  pace_through: string;
  budget: number;
  actual: number;
  projected: number;
  /** Each category's projected overspending, summed (underspending elsewhere doesn't offset it). */
  projected_over: number;
  drivers: RiskDriver[];
  counted: number;
};

export const riskQuery = queryOptions({
  queryKey: ["budget", "risk"],
  queryFn: () => api.get<Risk>("/budget/risk"),
});

export const riskLabel: Record<RiskLevel, string> = { low: "Low", moderate: "Moderate", high: "High", very_high: "Very high" };
/** Token text/stroke classes per level: green, amber, orange, red. */
export const riskTone: Record<RiskLevel, { text: string; stroke: string; bg: string }> = {
  low: { text: "text-positive", stroke: "stroke-positive", bg: "bg-positive" },
  moderate: { text: "text-warning", stroke: "stroke-warning", bg: "bg-warning" },
  high: { text: "text-accent", stroke: "stroke-accent", bg: "bg-accent" },
  very_high: { text: "text-negative", stroke: "stroke-negative", bg: "bg-negative" },
};

export type CloseoutStatus = { month: string; closes: string; closed: boolean };

export const closeoutStatusQuery = queryOptions({
  queryKey: ["budget", "closeout"],
  queryFn: () => api.get<CloseoutStatus>("/closeout"),
});

export type ReviewLine = { id: number; name: string; icon: string; group: string; kind: string; budget: number; actual: number; diff: number; hidden?: boolean };

export type Review = {
  month: string;
  budget: number;
  actual: number;
  net: number;
  surplus: number;
  over: ReviewLine[];
  under: ReviewLine[];
  on_target: number;
  non_monthly: ReviewLine[];
  income_budget: number;
  income_actual: number;
  goals_budget: number;
  goals_actual: number;
};

export type NextLine = {
  id: number;
  name: string;
  icon: string;
  group: string;
  budget: number;
  last_actual: number;
  average: number;
  recurring: number;
  expected: number;
  gap: number;
};

export type Outlook = { month: string; score: number; level: RiskLevel; budget: number; expected: number; over: number; at_risk: NextLine[]; months: number };

export type Destination = { id: number; name: string; icon: string; group: string; kind: "fixed" | "flexible" | "non_monthly" | "goal" };

export type Allocation = { category_id?: number; goal_id?: number; month?: string; amount: number; name: string; icon: string };

export type Closeout = {
  month: string;
  next_month: string;
  closes: string;
  /** Still inside the close-out window: can be closed, or undone. */
  can_change: boolean;
  review: Review;
  outlook: Outlook;
  destinations: Destination[];
  closed: { closed_at: number; closed_by: string; allocations: Allocation[]; analysis: string; analysis_at?: number } | null;
};

export const closeoutQuery = (month: string) =>
  queryOptions({
    queryKey: ["budget", "closeout", month],
    queryFn: () => api.get<Closeout>(`/closeout/${month}`),
  });
