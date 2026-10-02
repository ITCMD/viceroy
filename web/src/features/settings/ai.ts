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
