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
    <div role="tablist" className={clsx("flex gap-5 overflow-x-auto border-b border-border", className)}>
      {items.map((it) => (
        <button
          key={it.value}
          role="tab"
          aria-selected={value === it.value}
          onClick={() => onChange(it.value)}
          className={clsx(
            "-mb-px shrink-0 border-b-2 pb-2.5 pt-1 text-[14px] font-medium transition",
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
