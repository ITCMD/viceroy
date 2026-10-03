import { api } from "@/lib/api";

export type AISettings = {
  can_edit: boolean;
  key_set: boolean;
  key_hint: string;
  key_source: "settings" | "config" | "";
  chat_model: string;
  email_model: string;
  email_base_url: string;
  vision_model: string;
  categorize: boolean;
  categorize_review: boolean;
  config_chat_model: string;
  chat_ready: boolean;
  email_ready: boolean;
  vision_ready: boolean;
};

export const aiSettingsQuery = { queryKey: ["settings", "ai"], queryFn: () => api.get<AISettings>("/settings/ai") };

/** A model the AI endpoint offers. Prices are dollars per million tokens ("" = unknown). */
export type ModelInfo = {
  id: string;
  name: string;
  context: number;
  prompt_price: string;
  completion_price: string;
  images: boolean;
  tools: boolean;
};

export const modelsQuery = (endpoint: "openrouter" | "email", enabled: boolean) => ({
  queryKey: ["ai-models", endpoint],
  queryFn: () => api.get<{ models: ModelInfo[] }>(`/settings/ai/models?endpoint=${endpoint}`),
  enabled,
  staleTime: 30 * 60_000,
  retry: false,
});

export type AIUsageMonth = {
  month: string;
  requests: number;
  /** Millionths of a dollar, as OpenRouter reported. */
  cost_micros: number;
  /** Requests with no reported cost. */
  unpriced: number;
  features: { feature: string; requests: number; cost_micros: number; unpriced: number; tokens: number }[];
};

export const aiUsageQuery = { queryKey: ["settings", "ai", "usage"], queryFn: () => api.get<{ month: AIUsageMonth; previous: AIUsageMonth }>("/settings/ai/usage") };

export const featureLabels: Record<string, string> = {
  chat: "Chat",
  email: "Reading bank emails",
  categorize: "Categorizing transactions",
  vision: "Reading budgets & screenshots",
  budget_import: "Budget import",
  colors: "Bank colors",
  wishlist: "Wishlist prices",
  test: "Connection tests",
};
