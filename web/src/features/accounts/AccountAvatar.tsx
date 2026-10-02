import clsx from "clsx";
import type { Account } from "./api";

/** Black or white text, whichever reads better on the color. */
function inkFor(hex: string) {
  const n = parseInt(hex.slice(1), 16);
  const [r, g, b] = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((c) => {
    const v = c / 255;
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b > 0.4 ? "#111" : "#fff";
}

/** The account's logo, or its bank color with the first letter. Colors are user data, so they're inline styles. */
export function AccountAvatar({ account, size = 32, className }: { account: Pick<Account, "name" | "institution_name" | "color" | "logo_url">; size?: number; className?: string }) {
  const letter = (account.institution_name || account.name).slice(0, 1).toUpperCase();
  const box = { width: size, height: size };
  if (account.logo_url) {
    return <img src={account.logo_url} alt="" style={box} className={clsx("shrink-0 rounded-full border border-border bg-white object-contain", className)} data-testid="account-logo" />;
  }
  const color = /^#[0-9a-f]{6}$/i.test(account.color) ? account.color : "";
  return (
    <span
      style={color ? { ...box, background: color, color: inkFor(color) } : box}
      className={clsx("grid shrink-0 place-items-center rounded-full border border-border text-xs font-semibold", !color && "bg-surface-2 text-muted", className)}
      data-testid="account-avatar"
      data-color={color}
    >
      {letter}
    </span>
  );
}
