import clsx from "clsx";
import { Check, ChevronDown, Image as ImageIcon, Search, Wrench } from "lucide-react";
import { Popover } from "radix-ui";
import { useId, useState, type ReactNode } from "react";
import type { ModelInfo } from "./ai";

const MAX_SHOWN = 100;

const contextLabel = (n: number) => (n >= 1_000_000 ? `${+(n / 1_000_000).toFixed(1)}M` : n >= 1000 ? `${Math.round(n / 1000)}K` : n ? String(n) : "");

/** "$3 / $15", "Free", or "" when the endpoint has no prices. Prices are per million tokens. */
export function priceLabel(m: ModelInfo) {
  if (m.prompt_price === "" && m.completion_price === "") return "";
  if (m.prompt_price === "0" && m.completion_price === "0") return "Free";
  return `$${m.prompt_price || "?"} / $${m.completion_price || "?"}`;
}

/**
 * Field-styled button that opens a searchable list of the endpoint's models (OpenRouter's
 * catalog), filtered by default to models that can do the job (tool calling, image input).
 * Any other id can still be typed in.
 */
export function ModelPicker({
  label,
  value,
  onChange,
  models,
  error,
  need,
  emptyLabel,
  hint,
  disabled,
}: {
  label: string;
  value: string;
  onChange: (id: string) => void;
  models: ModelInfo[] | undefined;
  error?: string;
  /** Capability the default filter requires. */
  need?: "tools" | "images";
  /** When set, "" is an option shown with this label (e.g. "Same as chat model"). */
  emptyLabel?: string;
  hint?: string;
  disabled?: boolean;
}) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [onlyCapable, setOnlyCapable] = useState(true);
  const current = models?.find((m) => m.id === value);
  const needle = q.trim().toLowerCase();
  const filtered = (models ?? []).filter(
    (m) => (!needle || m.id.toLowerCase().includes(needle) || m.name.toLowerCase().includes(needle)) && (!need || !onlyCapable || m[need]),
  );
  const exact = !!needle && (models ?? []).some((m) => m.id.toLowerCase() === needle);
  const pick = (v: string) => {
    onChange(v);
    setOpen(false);
    setQ("");
  };

  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-[13px] font-medium text-text">
        {label}
      </label>
      {/* modal: its own scroll lock, so the wheel scrolls the list even inside a Dialog/Sheet (whose lock would swallow it). */}
      <Popover.Root modal open={open} onOpenChange={setOpen}>
        <Popover.Trigger
          id={id}
          disabled={disabled}
          className="flex h-9 w-full items-center justify-between gap-2 rounded-lg border border-border bg-surface px-3 text-left text-sm outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20 disabled:opacity-60"
        >
          <span className={clsx("truncate", !value && "text-muted")}>{current?.name ?? (value || emptyLabel || "Choose a model")}</span>
          <ChevronDown size={14} className="shrink-0 text-muted" />
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Content
            align="start"
            sideOffset={4}
            className="z-[60] flex max-h-[min(26rem,var(--radix-popover-content-available-height))] w-[min(30rem,calc(100vw-2rem))] flex-col overflow-hidden rounded-lg border border-border bg-surface shadow-xl"
          >
            <div className="flex items-center gap-2 border-b border-border px-3">
              <Search size={14} className="text-muted" />
              <input
                autoFocus
                value={q}
                onChange={(e) => setQ(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && needle) {
                    e.preventDefault();
                    pick(filtered.length === 1 ? filtered[0].id : q.trim());
                  }
                }}
                placeholder="Search models or type an id"
                aria-label="Search models"
                className="h-9 flex-1 bg-transparent text-sm outline-none placeholder:text-muted"
              />
            </div>
            {need && (
              <label className="flex items-center gap-2 border-b border-border px-3 py-1.5 text-xs text-muted">
                <input type="checkbox" checked={onlyCapable} onChange={(e) => setOnlyCapable(e.target.checked)} className="accent-accent" />
                {need === "images" ? "Only models that accept images" : "Only models with tool calling"}
              </label>
            )}
            <div className="overflow-y-auto py-1" role="listbox" aria-label={label}>
              {emptyLabel !== undefined && !needle && (
                <Row selected={value === ""} onClick={() => pick("")}>
                  <span className="flex-1 text-muted">{emptyLabel}</span>
                </Row>
              )}
              {needle && !exact && (
                <Row selected={false} onClick={() => pick(q.trim())}>
                  <span className="flex-1 truncate">
                    Use <span className="font-medium">{q.trim()}</span>
                  </span>
                </Row>
              )}
              {filtered.slice(0, MAX_SHOWN).map((m) => (
                <Row key={m.id} selected={m.id === value} onClick={() => pick(m.id)}>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate">{m.name}</span>
                    <span className="block truncate text-xs text-muted">{m.id}</span>
                  </span>
                  <span className="flex shrink-0 flex-col items-end text-xs text-muted">
                    <span>{priceLabel(m)}</span>
                    <span className="flex items-center gap-1">
                      {m.images && <ImageIcon size={11} aria-label="Accepts images" />}
                      {m.tools && <Wrench size={11} aria-label="Tool calling" />}
                      {contextLabel(m.context)}
                    </span>
                  </span>
                </Row>
              ))}
              {filtered.length > MAX_SHOWN && (
                <p className="px-3 py-2 text-center text-xs text-muted">{filtered.length - MAX_SHOWN} more, keep typing to narrow it down.</p>
              )}
              {!models && !error && <p className="px-3 py-4 text-center text-[13px] text-muted">Loading models…</p>}
              {error && <p className="px-3 py-3 text-center text-[13px] text-muted">{error} You can still type a model id.</p>}
              {models && filtered.length === 0 && !needle && <p className="px-3 py-4 text-center text-[13px] text-muted">No models.</p>}
            </div>
            <p className="border-t border-border px-3 py-1.5 text-[11px] text-muted">Prices per million tokens, input / output.</p>
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
      {hint && <p className="text-xs text-muted">{hint}</p>}
    </div>
  );
}

function Row({ selected, onClick, children }: { selected: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      role="option"
      aria-selected={selected}
      onClick={onClick}
      className={clsx("flex w-full items-center gap-3 px-3 py-1.5 text-left text-sm hover:bg-surface-2", selected && "bg-surface-2/60 font-medium")}
    >
      {children}
      {selected && <Check size={14} className="shrink-0 text-accent" />}
    </button>
  );
}
