import clsx from "clsx";

export type Logo = "butterfly" | "classic";

const storageKey = "viceroy.logo";

const icons: Record<Logo, { favicon: string; type: string; touch: string }> = {
  butterfly: { favicon: "/favicon-butterfly.png", type: "image/png", touch: "/apple-touch-butterfly.png" },
  classic: { favicon: "/icon.svg", type: "image/svg+xml", touch: "/icon.svg" },
};

/** The last logo this browser saw, so pages shown before sign-in (and the favicon) match. */
export function cachedLogo(): Logo {
  try {
    return localStorage.getItem(storageKey) === "classic" ? "classic" : "butterfly";
  } catch {
    return "butterfly";
  }
}

/** Remembers the household's logo choice and points the favicon at it. */
export function applyLogo(logo: Logo) {
  try {
    localStorage.setItem(storageKey, logo);
  } catch {
    // private mode etc.: the favicon still updates for this page
  }
  const i = icons[logo];
  const icon = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
  if (icon) {
    icon.href = i.favicon;
    icon.type = i.type;
  }
  const touch = document.querySelector<HTMLLinkElement>('link[rel="apple-touch-icon"]');
  if (touch) touch.href = i.touch;
}

/** The app mark. `size` is the height in px; the butterfly is wider than it is tall. */
export function AppLogo({ logo, size, className }: { logo: Logo; size: number; className?: string }) {
  return logo === "butterfly" ? (
    <img src="/logo-butterfly.png" alt="" style={{ height: size }} className={clsx("w-auto", className)} />
  ) : (
    <img src="/icon.svg" alt="" style={{ height: size, width: size }} className={className} />
  );
}
