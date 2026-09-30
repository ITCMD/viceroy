import clsx from "clsx";
import { Check, ChevronDown, Search } from "lucide-react";
import { Popover } from "radix-ui";
import { useId, useMemo, useState } from "react";
import { CategoryPill } from "./CategoryPill";

type Cat = { id: number; name: string; icon: string };
type Group = { id: number; name: string; categories: Cat[] };

/** Field-styled button that opens a searchable, grouped category list. */
export function CategoryPicker({
  label,
  groups,
  value,
  onChange,
  allowNone = true,
  noneLabel = "Uncategorized",
}: {
  label: string;
  groups: Group[];
  value: number | null;
  onChange: (id: number | null) => void;
  allowNone?: boolean;
  noneLabel?: string;
}) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const current = useMemo(() => groups.flatMap((g) => g.categories).find((c) => c.id === value), [groups, value]);
  const needle = q.trim().toLowerCase();
  const filtered = groups
    .map((g) => ({ ...g, categories: g.categories.filter((c) => !needle || c.name.toLowerCase().includes(needle)) }))
    .filter((g) => g.categories.length > 0);
  const pick = (v: number | null) => {
    onChange(v);
    setOpen(false);
    setQ("");
  };

  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-[13px] font-medium text-text">
        {label}
      </label>
      <Popover.Root open={open} onOpenChange={setOpen}>
        <Popover.Trigger
          id={id}
          className="flex h-9 w-full items-center justify-between gap-2 rounded-lg border border-border bg-surface px-3 text-left text-sm outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
        >
          <CategoryPill name={current?.name ?? (value === null ? noneLabel : "")} icon={current?.icon} className="text-sm" />
          <ChevronDown size={14} className="shrink-0 text-muted" />
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Content
            align="start"
            sideOffset={4}
            className="z-[60] flex max-h-[min(24rem,var(--radix-popover-content-available-height))] w-[var(--radix-popover-trigger-width)] min-w-64 flex-col overflow-hidden rounded-lg border border-border bg-surface shadow-xl"
          >
            <div className="flex items-center gap-2 border-b border-border px-3">
              <Search size={14} className="text-muted" />
              <input
                autoFocus
                value={q}
                onChange={(e) => setQ(e.target.value)}
                placeholder="Search categories"
                aria-label="Search categories"
                className="h-9 flex-1 bg-transparent text-sm outline-none placeholder:text-muted"
              />
            </div>
            <div className="overflow-y-auto py-1" role="listbox" aria-label={label}>
              {allowNone && !needle && <Option selected={value === null} onClick={() => pick(null)} label={noneLabel} />}
              {filtered.map((g) => (
                <div key={g.id}>
                  <div className="px-3 pb-1 pt-2 text-[11px] font-semibold uppercase tracking-wide text-muted">{g.name}</div>
                  {g.categories.map((c) => (
                    <Option key={c.id} selected={c.id === value} onClick={() => pick(c.id)} label={c.name} icon={c.icon} />
                  ))}
                </div>
              ))}
              {filtered.length === 0 && needle && <p className="px-3 py-4 text-center text-[13px] text-muted">No matching categories.</p>}
            </div>
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
    </div>
  );
}

function Option({ selected, onClick, label, icon }: { selected: boolean; onClick: () => void; label: string; icon?: string }) {
  return (
    <button
      type="button"
      role="option"
      aria-selected={selected}
      onClick={onClick}
      className={clsx("flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-surface-2", selected && "font-medium")}
    >
      <span aria-hidden className="w-5 text-center">{icon}</span>
      <span className="flex-1 truncate">{label}</span>
      {selected && <Check size={14} className="text-accent" />}
    </button>
  );
}
