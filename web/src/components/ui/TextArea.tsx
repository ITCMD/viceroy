import clsx from "clsx";
import { useId, type TextareaHTMLAttributes } from "react";

/** Multi-line Field. */
export function TextArea({ label, hint, className, ...props }: TextareaHTMLAttributes<HTMLTextAreaElement> & { label: string; hint?: string }) {
  const id = useId();
  return (
    <div className={clsx("flex flex-col gap-1", className)}>
      <label htmlFor={id} className="text-[13px] font-medium text-text">
        {label}
      </label>
      <textarea
        id={id}
        rows={3}
        className="resize-y rounded-lg border border-border bg-surface px-3 py-2 text-sm outline-none transition placeholder:text-muted focus:border-accent focus:ring-2 focus:ring-accent/20"
        {...props}
      />
      {hint && <p className="text-xs text-muted">{hint}</p>}
    </div>
  );
}
