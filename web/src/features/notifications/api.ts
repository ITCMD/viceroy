import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type NotificationItem = {
  id: number;
  kind: "over_budget" | "pacing" | "large_txn" | "disconnected" | "test";
  title: string;
  body: string;
  url: string;
  created_at: number;
  read: boolean;
};

export type NotifyPrefs = {
  over_budget: boolean;
  pacing: boolean;
  pacing_pct: number;
  large_txn: boolean;
  large_txn_cents: number;
  disconnected: boolean;
};

export type Device = { id: number; endpoint: string; user_agent: string; created_at: number };

export type NotifySettings = { public_key: string; prefs: NotifyPrefs; devices: Device[] };

export const notificationsQuery = queryOptions({
  queryKey: ["notifications"],
  queryFn: () => api.get<{ notifications: NotificationItem[]; unread: number }>("/notifications"),
  refetchInterval: 60_000,
});

export const notifySettingsQuery = queryOptions({
  queryKey: ["notifications", "settings"],
  queryFn: () => api.get<NotifySettings>("/notifications/settings"),
});

export function useMarkRead() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post("/notifications/read"),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notifications"], exact: true }),
  });
}

/** "Chrome on macOS" from a user agent string, for the device list. */
export function deviceName(ua: string) {
  const browser = /Edg\//.test(ua)
    ? "Edge"
    : /Firefox\//.test(ua)
      ? "Firefox"
      : /Chrome\//.test(ua)
        ? "Chrome"
        : /Safari\//.test(ua)
          ? "Safari"
          : "Browser";
  const os = /iPhone|iPad/.test(ua)
    ? "iOS"
    : /Android/.test(ua)
      ? "Android"
      : /Mac OS X/.test(ua)
        ? "macOS"
        : /Windows/.test(ua)
          ? "Windows"
          : /Linux/.test(ua)
            ? "Linux"
            : "";
  return os ? `${browser} on ${os}` : browser;
}
