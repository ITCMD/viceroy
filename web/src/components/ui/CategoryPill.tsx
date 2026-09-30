import clsx from "clsx";

/** Round emoji badge for a category; a muted "?" when uncategorized. */
export function CategoryIcon({ icon, size = "md" }: { icon?: string; size?: "sm" | "md" }) {
  return (
    <span
      aria-hidden
      className={clsx(
        "grid shrink-0 place-items-center rounded-full bg-surface-2",
        size === "sm" ? "size-6 text-xs" : "size-8 text-sm",
        !icon && "text-muted",
      )}
    >
      {icon || "?"}
    </span>
  );
}

/** Category name with its emoji, or "Uncategorized". */
export function CategoryPill({ name, icon, className }: { name?: string; icon?: string; className?: string }) {
  return (
    <span className={clsx("inline-flex min-w-0 items-center gap-1.5 text-[13px]", !name && "text-muted", className)}>
      {icon && <span aria-hidden>{icon}</span>}
      <span className="truncate">{name || "Uncategorized"}</span>
    </span>
  );
}
