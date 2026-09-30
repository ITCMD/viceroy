import clsx from "clsx";
import { Dialog as D } from "radix-ui";
import { X } from "lucide-react";
import type { ReactNode } from "react";

/** Centered modal. `footer` renders right-aligned below the body (Cancel / Save). */
export function Dialog({
  open,
  onOpenChange,
  title,
  description,
  footer,
  children,
  className,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  footer?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <D.Root open={open} onOpenChange={onOpenChange}>
      <D.Portal>
        <D.Overlay className="fixed inset-0 z-40 bg-black/40" />
        <D.Content
          className={clsx(
            "fixed left-1/2 top-1/2 z-50 flex max-h-[90vh] w-[calc(100vw-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 flex-col rounded-xl border border-border bg-surface shadow-xl outline-none",
            className,
          )}
        >
          <header className="flex items-start justify-between gap-4 border-b border-border px-5 py-4">
            <div>
              <D.Title className="text-[15px] font-semibold">{title}</D.Title>
              {description ? (
                <D.Description className="mt-0.5 text-[13px] text-muted">{description}</D.Description>
              ) : (
                <D.Description className="sr-only">{title}</D.Description>
              )}
            </div>
            <D.Close className="rounded-md p-1 text-muted hover:bg-surface-2 hover:text-text" aria-label="Close">
              <X size={16} />
            </D.Close>
          </header>
          <div className="overflow-y-auto px-5 py-4">{children}</div>
          {footer && <footer className="flex justify-end gap-2 border-t border-border px-5 py-3">{footer}</footer>}
        </D.Content>
      </D.Portal>
    </D.Root>
  );
}

/** Right-hand side panel (full screen on mobile) for viewing and editing one record. */
export function Sheet({
  open,
  onOpenChange,
  title,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  children: ReactNode;
}) {
  return (
    <D.Root open={open} onOpenChange={onOpenChange}>
      <D.Portal>
        <D.Overlay className="fixed inset-0 z-40 bg-black/30" />
        <D.Content className="fixed inset-y-0 right-0 z-50 flex w-full max-w-md flex-col border-l border-border bg-surface shadow-xl outline-none">
          <header className="flex h-14 shrink-0 items-center justify-between border-b border-border px-5">
            <D.Title className="truncate text-[15px] font-semibold">{title}</D.Title>
            <D.Description className="sr-only">Details</D.Description>
            <D.Close className="rounded-md p-1 text-muted hover:bg-surface-2 hover:text-text" aria-label="Close">
              <X size={16} />
            </D.Close>
          </header>
          <div className="flex-1 overflow-y-auto p-5">{children}</div>
        </D.Content>
      </D.Portal>
    </D.Root>
  );
}
