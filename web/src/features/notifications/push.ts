import { api } from "@/lib/api";
import type { Device } from "./api";

/** Why push can't be used in this browser, or null when it can. */
export function pushUnavailableReason(): string | null {
  if (!window.isSecureContext) return "https";
  if (!("serviceWorker" in navigator) || !("PushManager" in window) || !("Notification" in window)) return "unsupported";
  if (Notification.permission === "denied") return "denied";
  return null;
}

function keyBytes(base64url: string) {
  const pad = "=".repeat((4 - (base64url.length % 4)) % 4);
  const raw = atob((base64url + pad).replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from(raw, (c) => c.charCodeAt(0));
}

async function registration() {
  const reg = await Promise.race([
    navigator.serviceWorker.ready,
    new Promise<never>((_, reject) => setTimeout(() => reject(new Error("The app's service worker isn't running. Reload the page and try again.")), 8000)),
  ]);
  return reg;
}

/** The push endpoint of this browser, if it is subscribed. */
export async function currentEndpoint(): Promise<string | null> {
  if (pushUnavailableReason()) return null;
  const reg = await navigator.serviceWorker.getRegistration();
  const sub = await reg?.pushManager.getSubscription();
  return sub?.endpoint ?? null;
}

/** Asks for permission, subscribes this browser and registers it with the server. */
export async function enablePush(publicKey: string): Promise<Device> {
  const perm = await Notification.requestPermission();
  if (perm !== "granted") throw new Error("Notifications weren't allowed. You can change this in the browser's site settings.");
  const reg = await registration();
  let sub = await reg.pushManager.getSubscription();
  if (!sub) sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(publicKey) });
  return api.post<Device>("/notifications/subscriptions", sub.toJSON());
}

/** Unsubscribes this browser (if it is the given device) and removes the device on the server. */
export async function removeDevice(device: Device) {
  if ((await currentEndpoint()) === device.endpoint) {
    const reg = await navigator.serviceWorker.getRegistration();
    await (await reg?.pushManager.getSubscription())?.unsubscribe();
  }
  await api.del(`/notifications/subscriptions/${device.id}`);
}
