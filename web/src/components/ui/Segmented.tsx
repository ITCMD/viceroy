import clsx from "clsx";

/** Compact pill toggle for a few mutually exclusive options (time ranges, filters, modes). */
export function Segmented<T extends string | number>({
  value,
  onChange,
  items,
  label,
  className,
}: {
  value: T;
  onChange: (v: T) => void;
  items: { value: T; label: string }[];
  label: string;
  className?: string;
}) {
  return (
    <div className={clsx("flex rounded-lg border border-border bg-surface p-0.5", className)} role="group" aria-label={label}>
      {items.map((it) => (
        <button
          key={it.value}
          type="button"
          onClick={() => onChange(it.value)}
          aria-pressed={it.value === value}
          className={clsx(
            "flex-1 whitespace-nowrap rounded-md px-2.5 py-1 text-xs font-medium",
            it.value === value ? "bg-surface-2 text-text" : "text-muted hover:text-text",
          )}
        >
          {it.label}
        </button>
      ))}
    </div>
  );
}
