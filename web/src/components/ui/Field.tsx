import clsx from "clsx";
import { useId, type InputHTMLAttributes } from "react";

export function Field({
  label,
  hint,
  hideLabel,
  className,
  ...props
}: InputHTMLAttributes<HTMLInputElement> & { label: string; hint?: string; hideLabel?: boolean }) {
  const id = useId();
  return (
    <div className={clsx("flex flex-col gap-1", className)}>
      <label htmlFor={id} className={hideLabel ? "sr-only" : "text-[13px] font-medium text-text"}>
        {label}
      </label>
      <input
        id={id}
        className="h-9 rounded-lg border border-border bg-surface px-3 text-sm outline-none transition placeholder:text-muted focus:border-accent focus:ring-2 focus:ring-accent/20"
        {...props}
      />
      {hint && <p className="text-xs text-muted">{hint}</p>}
    </div>
  );
}
