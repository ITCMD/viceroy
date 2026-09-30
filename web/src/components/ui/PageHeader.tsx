import type { ReactNode } from "react";

/** Top bar of every page: title on the left, page actions on the right. */
export function PageHeader({ title, actions }: { title: ReactNode; actions?: ReactNode }) {
  return (
    <div className="sticky top-0 z-10 flex h-14 items-center justify-between border-b border-border bg-bg/90 px-4 backdrop-blur md:px-6">
      <h1 className="text-lg font-semibold">{title}</h1>
      <div className="flex items-center gap-2">{actions}</div>
    </div>
  );
}
