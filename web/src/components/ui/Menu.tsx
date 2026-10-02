import { DropdownMenu } from "radix-ui";
import { MoreHorizontal, type LucideIcon } from "lucide-react";

/** "More" button with a short list of actions. */
export function Menu({ label, items }: { label: string; items: { label: string; icon?: LucideIcon; onSelect: () => void; disabled?: boolean }[] }) {
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger
        aria-label={label}
        className="inline-flex size-8 items-center justify-center rounded-lg border border-border bg-surface text-muted hover:bg-surface-2 hover:text-text"
      >
        <MoreHorizontal size={16} />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content align="end" sideOffset={4} className="z-[60] min-w-48 rounded-lg border border-border bg-surface p-1 shadow-xl">
          {items.map((it) => {
            const Icon = it.icon;
            return (
              <DropdownMenu.Item
                key={it.label}
                disabled={it.disabled}
                onSelect={it.onSelect}
                className="flex cursor-pointer items-center gap-2 rounded-md px-2.5 py-1.5 text-[13px] outline-none data-[disabled]:opacity-50 data-[highlighted]:bg-surface-2"
              >
                {Icon && <Icon size={14} className="text-muted" />}
                {it.label}
              </DropdownMenu.Item>
            );
          })}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}
