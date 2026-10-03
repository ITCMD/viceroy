import { useQuery } from "@tanstack/react-query";
import { ChevronDown, X } from "lucide-react";
import { useEffect, useState } from "react";
import { Button, CategoryPicker, Field, Segmented, withIcon } from "@/components/ui";
import { goalsQuery } from "@/features/goals/api";
import { formatMoney } from "@/lib/format";
import { categoriesQuery, shortDate, tagsQuery, type TxnLink } from "./api";

const pad = (n: number) => String(n).padStart(2, "0");
const iso = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

/** Quick date ranges for the filter panel. */
function presets(now = new Date()): { label: string; from: string; to: string }[] {
  const y = now.getFullYear();
  const m = now.getMonth();
  return [
    { label: "This month", from: iso(new Date(y, m, 1)), to: iso(new Date(y, m + 1, 0)) },
    { label: "Last month", from: iso(new Date(y, m - 1, 1)), to: iso(new Date(y, m, 0)) },
    { label: "Last 90 days", from: iso(new Date(y, m, now.getDate() - 89)), to: iso(now) },
    { label: "This year", from: `${y}-01-01`, to: `${y}-12-31` },
    { label: "Last year", from: `${y - 1}-01-01`, to: `${y - 1}-12-31` },
  ];
}

const select =
  "h-9 w-full appearance-none rounded-lg border border-border bg-surface pl-3 pr-8 text-sm outline-none focus:border-accent focus:ring-2 focus:ring-accent/20";

/** Filters beyond search and account: type, category, amount, dates and tag. Edits the URL filters. */
export function TxnFilterPanel({ link, onChange }: { link: TxnLink; onChange: (next: TxnLink) => void }) {
  const { data: cats } = useQuery(categoriesQuery);
  const { data: tags } = useQuery(tagsQuery);
  const set = (patch: Partial<TxnLink>) => onChange({ ...link, ...patch });
  // Amounts apply after a pause so typing doesn't refetch on every key.
  const [min, setMin] = useState(link.min?.toString() ?? "");
  const [max, setMax] = useState(link.max?.toString() ?? "");
  useEffect(() => {
    setMin(link.min?.toString() ?? "");
    setMax(link.max?.toString() ?? "");
  }, [link.min, link.max]);
  useEffect(() => {
    const num = (v: string) => (v.trim() !== "" && Number(v.replace(/[$,]/g, "")) >= 0 ? Number(v.replace(/[$,]/g, "")) : undefined);
    const id = setTimeout(() => {
      if (num(min) !== link.min || num(max) !== link.max) onChange({ ...link, min: num(min), max: num(max) });
    }, 400);
    return () => clearTimeout(id);
  }, [min, max, link, onChange]);

  return (
    <div className="grid gap-3 rounded-xl border border-border bg-surface p-4 sm:grid-cols-2" data-testid="txn-filter-panel">
      <div className="flex flex-col gap-1">
        <span className="text-[13px] font-medium">Type</span>
        <Segmented
          label="Type"
          value={link.direction ?? "all"}
          onChange={(v) => set({ direction: v === "all" ? undefined : v })}
          items={[
            { value: "all", label: "All" },
            { value: "out", label: "Expenses" },
            { value: "in", label: "Income" },
          ]}
        />
      </div>
      <div className="flex flex-col gap-1">
        <CategoryPicker
          label="Category"
          groups={cats?.groups ?? []}
          value={link.category ?? null}
          onChange={(id) => set({ category: id ?? undefined, group: undefined, uncategorized: undefined })}
          noneLabel="Any category"
        />
      </div>
      <div className="flex gap-2">
        <Field label="Min $" inputMode="decimal" placeholder="0" value={min} onChange={(e) => setMin(e.target.value)} className="min-w-0 flex-1" />
        <Field label="Max $" inputMode="decimal" placeholder="Any" value={max} onChange={(e) => setMax(e.target.value)} className="min-w-0 flex-1" />
      </div>
      <label className="flex flex-col gap-1">
        <span className="text-[13px] font-medium">Tag</span>
        <span className="relative">
          <select value={link.tag ?? 0} onChange={(e) => set({ tag: Number(e.target.value) || undefined })} className={select}>
            <option value={0}>Any tag</option>
            {tags?.tags.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
          <ChevronDown size={14} className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-muted" />
        </span>
      </label>
      <div className="flex flex-col gap-2 sm:col-span-2">
        <div className="flex flex-wrap items-end gap-2">
          <Field label="From" type="date" value={link.from ?? ""} onChange={(e) => set({ from: e.target.value || undefined })} />
          <Field label="To" type="date" value={link.to ?? ""} onChange={(e) => set({ to: e.target.value || undefined })} />
          <div className="flex flex-wrap gap-1.5 pb-0.5">
            {presets().map((p) => (
              <button
                key={p.label}
                type="button"
                onClick={() => set({ from: p.from, to: p.to })}
                aria-pressed={link.from === p.from && link.to === p.to}
                className="rounded-full border border-border px-2.5 py-1 text-xs text-muted hover:bg-surface-2 hover:text-text aria-pressed:border-accent/50 aria-pressed:bg-accent-soft aria-pressed:text-text"
              >
                {p.label}
              </button>
            ))}
          </div>
        </div>
      </div>
      {Object.keys(link).length > 0 && (
        <div className="sm:col-span-2">
          <Button variant="ghost" size="sm" onClick={() => onChange({})}>
            Clear all filters
          </Button>
        </div>
      )}
    </div>
  );
}

/** How many filters are set, counting a date range or an amount range once. */
export function filterCount(link: TxnLink) {
  const keys = new Set(Object.keys(link).map((k) => (k === "to" ? "from" : k === "max" ? "min" : k)));
  return keys.size;
}

/** The URL filters as removable chips (also what a link from a report or budget line set). */
export function TxnFilterChips({ link, onChange }: { link: TxnLink; onChange: (next: TxnLink) => void }) {
  const { data: cats } = useQuery({ ...categoriesQuery, enabled: !!(link.category || link.group) });
  const { data: goals } = useQuery({ ...goalsQuery, enabled: !!link.goal });
  const { data: tags } = useQuery({ ...tagsQuery, enabled: !!link.tag });
  const cat = cats?.groups.flatMap((g) => g.categories).find((c) => c.id === link.category);
  const group = cats?.groups.find((g) => g.id === link.group);
  const goal = goals?.goals.find((g) => g.id === link.goal);
  const tag = tags?.tags.find((t) => t.id === link.tag);
  const usd = (v: number) => formatMoney(Math.round(v * 100), { whole: Number.isInteger(v) });
  const chips: { key: string; label: string; clear: Partial<TxnLink> }[] = [];
  if (link.category) chips.push({ key: "category", label: cat ? withIcon(cat.icon, cat.name) : "Category", clear: { category: undefined } });
  if (link.group) chips.push({ key: "group", label: group ? group.name : "Group", clear: { group: undefined } });
  if (link.goal) chips.push({ key: "goal", label: goal ? `${goal.icon} ${goal.name}` : "Goal", clear: { goal: undefined } });
  if (link.uncategorized) chips.push({ key: "uncategorized", label: "Uncategorized", clear: { uncategorized: undefined } });
  if (link.merchant) chips.push({ key: "merchant", label: link.merchant, clear: { merchant: undefined } });
  if (link.direction) chips.push({ key: "direction", label: link.direction === "in" ? "Income" : "Expenses", clear: { direction: undefined } });
  if (link.min !== undefined || link.max !== undefined) {
    const label = link.min !== undefined && link.max !== undefined ? `${usd(link.min)} – ${usd(link.max)}` : link.min !== undefined ? `${usd(link.min)} or more` : `Up to ${usd(link.max!)}`;
    chips.push({ key: "amount", label, clear: { min: undefined, max: undefined } });
  }
  if (link.from || link.to) {
    const label = link.from && link.to ? `${shortDate(link.from)} – ${shortDate(link.to)}` : link.from ? `Since ${shortDate(link.from)}` : `Until ${shortDate(link.to!)}`;
    chips.push({ key: "dates", label, clear: { from: undefined, to: undefined } });
  }
  if (link.tag) chips.push({ key: "tag", label: tag ? `#${tag.name}` : "Tag", clear: { tag: undefined } });
  if (chips.length === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-1.5" data-testid="txn-link-filter">
      {chips.map((c) => (
        <span key={c.key} className="flex items-center gap-1 rounded-full border border-accent/40 bg-accent-soft py-0.5 pl-2.5 pr-1 text-[13px]" data-testid="txn-filter-chip">
          <span className="max-w-56 truncate font-medium">{c.label}</span>
          <button
            type="button"
            onClick={() => onChange(Object.fromEntries(Object.entries({ ...link, ...c.clear }).filter(([, v]) => v !== undefined)) as TxnLink)}
            aria-label={`Remove filter ${c.label}`}
            className="grid size-5 place-items-center rounded-full text-muted hover:bg-surface hover:text-text"
          >
            <X size={13} />
          </button>
        </span>
      ))}
      {chips.length > 1 && (
        <button type="button" onClick={() => onChange({})} className="px-1.5 text-xs text-muted hover:text-text">
          Clear all
        </button>
      )}
    </div>
  );
}
