import clsx from "clsx";
import type { ReactNode } from "react";

/** The standard content container. Title row is optional; `action` sits top-right. */
export function Card({
  title,
  action,
  className,
  children,
}: {
  title?: ReactNode;
  action?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section className={clsx("rounded-xl border border-border bg-surface", className)}>
      {(title || action) && (
        <header className="flex items-center justify-between border-b border-border px-4 py-3">
          <h2 className="text-[15px] font-semibold">{title}</h2>
          {action}
        </header>
      )}
      <div className="p-4">{children}</div>
    </section>
  );
}
