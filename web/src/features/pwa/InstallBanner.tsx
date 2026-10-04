import { Download, Share, SquarePlus, X } from "lucide-react";
import { useState } from "react";
import { AppLogo, cachedLogo } from "@/components/AppLogo";
import { Button } from "@/components/ui";
import { promptInstall, useInstallMode } from "@/lib/install";

const dismissKey = "viceroy.install.dismissed";
const snoozeMs = 14 * 24 * 60 * 60 * 1000;

function snoozed() {
  try {
    return Date.now() - Number(localStorage.getItem(dismissKey) ?? 0) < snoozeMs;
  } catch {
    return false;
  }
}

/** On a phone that hasn't installed Viceroy: a card above the tab bar offering to install it. */
export function InstallBanner() {
  const mode = useInstallMode();
  const [hidden, setHidden] = useState(snoozed);
  const [busy, setBusy] = useState(false);
  if (!mode || hidden) return null;

  const dismiss = () => {
    try {
      localStorage.setItem(dismissKey, String(Date.now()));
    } catch {}
    setHidden(true);
  };

  let body;
  if (mode === "prompt") {
    body = "Add it to your home screen for full-screen use and spending alerts.";
  } else if (mode === "ios") {
    body = (
      <>
        Tap <Share size={13} className="inline -translate-y-px" aria-label="Share" /> in Safari, then{" "}
        <span className="whitespace-nowrap">
          <SquarePlus size={13} className="inline -translate-y-px" /> Add to Home Screen
        </span>
        .{!window.isSecureContext && " Alerts on your phone need Viceroy served over HTTPS."}
      </>
    );
  } else if (mode === "insecure") {
    body =
      "Phones only install apps from secure (https://) addresses, and this one is http://. Ask whoever runs Viceroy to set up HTTPS (tls or public_url in viceroy.toml). Until then, Add to Home screen in the browser menu makes a shortcut.";
  } else {
    body = "Open your browser menu and choose Install app or Add to Home screen.";
  }

  return (
    <div
      role="region"
      aria-label="Install Viceroy"
      data-testid="install-banner"
      className="fixed inset-x-3 bottom-[calc(4.25rem+env(safe-area-inset-bottom))] z-30 flex items-start gap-3 rounded-xl border border-border bg-surface p-3 shadow-xl md:hidden"
    >
      <AppLogo logo={cachedLogo()} size={36} />
      <div className="min-w-0 flex-1">
        <p className="text-sm font-semibold">Install Viceroy</p>
        <p className="mt-0.5 text-[13px] text-muted">{body}</p>
        {mode === "prompt" && (
          <Button
            size="sm"
            className="mt-2"
            loading={busy}
            onClick={async () => {
              setBusy(true);
              const ok = await promptInstall();
              setBusy(false);
              if (ok) setHidden(true);
              else dismiss();
            }}
          >
            <Download size={14} /> Install
          </Button>
        )}
      </div>
      <button className="-m-1 grid size-7 shrink-0 place-items-center rounded-md text-muted hover:bg-surface-2 hover:text-text" aria-label="Not now" onClick={dismiss}>
        <X size={16} />
      </button>
    </div>
  );
}
