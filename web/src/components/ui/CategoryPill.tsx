import clsx from "clsx";

/** An uploaded image icon is stored as its URL (/api/categories/<id>/icon?v=…) instead of an emoji. */
export const isImageIcon = (icon?: string | null) => !!icon && icon.startsWith("/api/");

/** The icon as text, for labels that can't hold an image ("" for an uploaded image). */
export const iconText = (icon?: string | null) => (icon && !isImageIcon(icon) ? icon : "");

/** "🛒 Groceries", or just the name when the icon is an image. */
export const withIcon = (icon: string | null | undefined, name: string) => (iconText(icon) ? `${iconText(icon)} ${name}` : name);

/** An emoji, or an uploaded image sized like one. */
export function Glyph({ icon, className }: { icon?: string | null; className?: string }) {
  if (!icon) return null;
  if (isImageIcon(icon)) return <img src={icon} alt="" aria-hidden className={clsx("inline-block size-[1.15em] shrink-0 rounded-sm object-contain align-[-0.15em]", className)} />;
  return (
    <span aria-hidden className={className}>
      {icon}
    </span>
  );
}

/** Round emoji badge for a category; a muted "?" when uncategorized. */
export function CategoryIcon({ icon, size = "md" }: { icon?: string; size?: "sm" | "md" }) {
  return (
    <span
      aria-hidden
      className={clsx(
        "grid shrink-0 place-items-center overflow-hidden rounded-full bg-surface-2",
        size === "sm" ? "size-6 text-xs" : "size-8 text-sm",
        !icon && "text-muted",
      )}
    >
      {isImageIcon(icon) ? <img src={icon} alt="" className="size-[70%] object-contain" /> : icon || "?"}
    </span>
  );
}

/** Category name with its emoji, or "Uncategorized". */
export function CategoryPill({ name, icon, className }: { name?: string; icon?: string; className?: string }) {
  return (
    <span className={clsx("inline-flex min-w-0 items-center gap-1.5 text-[13px]", !name && "text-muted", className)}>
      <Glyph icon={icon} />
      <span className="truncate">{name || "Uncategorized"}</span>
    </span>
  );
}
