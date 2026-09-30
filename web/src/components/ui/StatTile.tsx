import clsx from "clsx";
import type { ReactNode } from "react";

/** A headline number with a label, used in rows of summary stats (reports, dashboard). */
export function StatTile({ label, children, sub, className }: { label: string; children: ReactNode; sub?: ReactNode; className?: string }) {
  return (
    <div className={clsx("rounded-xl border border-border bg-surface px-4 py-3", className)}>
      <div className="text-[13px] font-medium text-muted">{label}</div>
      <div className="mt-0.5 text-xl font-semibold tracking-tight tabular">{children}</div>
      {sub && <div className="mt-0.5 text-xs text-muted">{sub}</div>}
    </div>
  );
}
