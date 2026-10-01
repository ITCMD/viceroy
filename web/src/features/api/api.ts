import { queryOptions } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type ApiKey = { id: number; name: string; prefix: string; scope: "read" | "write"; created_by: string; created_at: number; last_used_at: number | null };
export type ApiSettings = { enabled: boolean; can_edit: boolean; keys: ApiKey[] };
export type ApiDoc = { group: string; method: string; path: string; summary: string; params?: string; access: "public" | "session" | "read" | "write" };

export const apiSettingsQuery = queryOptions({ queryKey: ["settings", "api"], queryFn: () => api.get<ApiSettings>("/settings/api") });
export const apiDocsQuery = queryOptions({
  queryKey: ["api-docs"],
  queryFn: () => api.get<{ base_path: string; endpoints: ApiDoc[] }>("/docs"),
  staleTime: Infinity,
});
