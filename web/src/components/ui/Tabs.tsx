import clsx from "clsx";

/** Underlined tab strip used at the top of pages (Accounts, Reports...). Controlled. */
export function Tabs<T extends string>({
  value,
  onChange,
  items,
  className,
}: {
  value: T;
  onChange: (v: T) => void;
  items: { value: T; label: string; count?: number }[];
  className?: string;
}) {
  return (
    // The baseline is an inset shadow rather than a border so the strip can scroll sideways on
    // narrow screens without the active underline overflowing it (which showed a vertical
    // scrollbar); the scrollbar itself is hidden.
    <div
      role="tablist"
      className={clsx(
        "flex gap-5 overflow-x-auto overflow-y-hidden shadow-[inset_0_-1px_0_var(--color-border)] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden",
        className,
      )}
    >
      {items.map((it) => (
        <button
          key={it.value}
          role="tab"
          aria-selected={value === it.value}
          onClick={() => onChange(it.value)}
          className={clsx(
            "shrink-0 border-b-2 pb-2.5 pt-1 text-[14px] font-medium transition",
            value === it.value ? "border-accent text-text" : "border-transparent text-muted hover:text-text",
          )}
        >
          {it.label}
          {it.count !== undefined && <span className="ml-1.5 text-xs text-muted">{it.count}</span>}
        </button>
      ))}
    </div>
  );
}
