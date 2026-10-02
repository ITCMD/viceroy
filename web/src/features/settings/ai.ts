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
  config_chat_model: string;
  chat_ready: boolean;
  email_ready: boolean;
  vision_ready: boolean;
};

export const aiSettingsQuery = { queryKey: ["settings", "ai"], queryFn: () => api.get<AISettings>("/settings/ai") };
