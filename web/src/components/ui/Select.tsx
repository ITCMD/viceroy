import clsx from "clsx";
import { ChevronDown } from "lucide-react";
import { useId, type SelectHTMLAttributes } from "react";

/** Native select styled like Field (native keeps mobile pickers and a11y for free). */
export function Select({
  label,
  options,
  className,
  ...props
}: SelectHTMLAttributes<HTMLSelectElement> & { label: string; options: { value: string; label: string }[] }) {
  const id = useId();
  return (
    <div className={clsx("flex flex-col gap-1", className)}>
      <label htmlFor={id} className="text-[13px] font-medium text-text">
        {label}
      </label>
      <div className="relative">
        <select
          id={id}
          className="h-9 w-full appearance-none rounded-lg border border-border bg-surface pl-3 pr-8 text-sm outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
          {...props}
        >
          {options.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
        <ChevronDown size={14} className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-muted" />
      </div>
    </div>
  );
}
