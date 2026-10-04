import { useSyncExternalStore } from "react";

/** Chrome's install event; not in the DOM typings. */
export type InstallPromptEvent = Event & {
  prompt: () => Promise<void>;
  userChoice: Promise<{ outcome: "accepted" | "dismissed" }>;
};

// The browser can fire beforeinstallprompt before React mounts, so the listener is set up when
// this module loads (main.tsx imports it) and the event is kept until the banner asks for it.
let deferred: InstallPromptEvent | null = null;
let installed = false;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());

if (typeof window !== "undefined") {
  window.addEventListener("beforeinstallprompt", (e) => {
    e.preventDefault(); // we show our own banner instead of Chrome's mini-infobar
    deferred = e as InstallPromptEvent;
    emit();
  });
  window.addEventListener("appinstalled", () => {
    deferred = null;
    installed = true;
    emit();
  });
}

export type InstallMode =
  | "prompt" // the browser handed us an install prompt
  | "ios" // Safari on iPhone/iPad: Share → Add to Home Screen
  | "insecure" // plain http: browsers won't install or allow push
  | "menu" // secure, but no prompt (yet): the browser menu has the option
  | null; // already installed, or not a phone

export function isStandalone() {
  return (
    window.matchMedia("(display-mode: standalone)").matches ||
    (navigator as Navigator & { standalone?: boolean }).standalone === true
  );
}

function isIOS() {
  // iPadOS reports itself as a Mac; touch support gives it away.
  return /iPhone|iPad|iPod/.test(navigator.userAgent) || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
}

function isPhone() {
  return isIOS() || /Android|Mobi/i.test(navigator.userAgent) || window.matchMedia("(pointer: coarse) and (max-width: 900px)").matches;
}

function mode(): InstallMode {
  if (installed || isStandalone() || !isPhone()) return null;
  if (deferred) return "prompt";
  if (isIOS()) return "ios";
  if (!window.isSecureContext) return "insecure";
  return "menu";
}

/** How this device can install Viceroy, kept current as the install events arrive. */
export function useInstallMode(): InstallMode {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    mode,
  );
}

/** Shows the browser's install dialog; true when the person accepted. */
export async function promptInstall() {
  const e = deferred;
  if (!e) return false;
  deferred = null; // a prompt event can only be used once
  await e.prompt();
  const { outcome } = await e.userChoice;
  emit();
  return outcome === "accepted";
}
