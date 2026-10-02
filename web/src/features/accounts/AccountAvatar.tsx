import clsx from "clsx";
import { inkFor } from "@/lib/color";
import type { Account } from "./api";

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
