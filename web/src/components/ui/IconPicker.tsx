import clsx from "clsx";
import { ImagePlus, Search } from "lucide-react";
import { Popover } from "radix-ui";
import { useId, useRef, useState } from "react";
import { shrinkImage } from "@/lib/image";
import { emojiGroups } from "./emoji";

const isImage = (v: string) => v.startsWith("/api/") || v.startsWith("data:");

/**
 * Field-styled icon button: click to pick an emoji (searchable), type one, or, with
 * allowUpload, upload an image. An upload comes back through onChange as a PNG data URL for
 * the caller to save after the item exists.
 */
export function IconPicker({ label = "Icon", value, onChange, allowUpload, placeholder = "📦" }: { label?: string; value: string; onChange: (v: string) => void; allowUpload?: boolean; placeholder?: string }) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [err, setErr] = useState("");
  const file = useRef<HTMLInputElement>(null);
  const needle = q.trim().toLowerCase();
  const groups = emojiGroups
    .map((g) => ({ ...g, items: g.items.filter(([e, words]) => !needle || words.includes(needle) || e === needle) }))
    .filter((g) => g.items.length > 0);
  const pick = (v: string) => {
    onChange(v);
    setOpen(false);
    setQ("");
  };
  const upload = async (f: File | undefined) => {
    if (!f) return;
    setErr("");
    try {
      pick(await shrinkImage(f, 96, "image/png"));
    } catch {
      setErr("That file isn't an image Viceroy can read.");
    }
  };
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-[13px] font-medium text-text">
        {label}
      </label>
      <Popover.Root modal open={open} onOpenChange={setOpen}>
        <Popover.Trigger
          id={id}
          className="grid h-9 w-full place-items-center rounded-lg border border-border bg-surface text-lg outline-none transition hover:bg-surface-2 focus:border-accent focus:ring-2 focus:ring-accent/20"
          aria-label={`${label}: choose`}
          data-testid="icon-picker"
        >
          {value ? isImage(value) ? <img src={value} alt="" className="size-6 rounded object-contain" /> : value : <span className="text-muted opacity-60">{placeholder}</span>}
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Content
            align="start"
            sideOffset={4}
            className="z-[60] flex max-h-[min(24rem,var(--radix-popover-content-available-height))] w-80 max-w-[calc(100vw-2rem)] flex-col overflow-hidden rounded-lg border border-border bg-surface shadow-xl"
          >
            <div className="flex items-center gap-2 border-b border-border px-3">
              <Search size={14} className="text-muted" />
              <input
                autoFocus
                value={q}
                onChange={(e) => setQ(e.target.value)}
                placeholder="Search, or type an emoji"
                aria-label="Search emoji"
                className="h-9 flex-1 bg-transparent text-sm outline-none placeholder:text-muted"
              />
            </div>
            <div className="overflow-y-auto px-2 pb-2" role="listbox" aria-label="Emoji">
              {/* Anything typed that isn't a search word (an emoji from the keyboard) can be used as is. */}
              {needle && groups.length === 0 && !/^[a-z0-9 ]+$/i.test(needle) && q.trim().length <= 8 && (
                <button type="button" className="mt-2 w-full rounded-md px-2 py-1.5 text-left text-sm hover:bg-surface-2" onClick={() => pick(q.trim())}>
                  Use {q.trim()}
                </button>
              )}
              {groups.map((g) => (
                <div key={g.name}>
                  <div className="px-1 pb-1 pt-2 text-[11px] font-semibold uppercase tracking-wide text-muted">{g.name}</div>
                  <div className="grid grid-cols-8 gap-0.5">
                    {g.items.map(([e, words]) => (
                      <button
                        key={g.name + e}
                        type="button"
                        role="option"
                        aria-selected={e === value}
                        title={words}
                        aria-label={words.split(" ")[0]}
                        onClick={() => pick(e)}
                        className={clsx("grid aspect-square place-items-center rounded-md text-lg hover:bg-surface-2", e === value && "bg-accent-soft")}
                      >
                        {e}
                      </button>
                    ))}
                  </div>
                </div>
              ))}
              {needle && groups.length === 0 && /^[a-z0-9 ]+$/i.test(needle) && <p className="px-1 py-4 text-center text-[13px] text-muted">No matching emoji.</p>}
            </div>
            {(allowUpload || value) && (
              <div className="flex items-center gap-2 border-t border-border px-2 py-1.5">
                {allowUpload && (
                  <>
                    <button type="button" className="flex items-center gap-1.5 rounded-md px-2 py-1 text-[13px] font-medium text-accent hover:bg-surface-2" onClick={() => file.current?.click()}>
                      <ImagePlus size={14} /> Upload image
                    </button>
                    <input ref={file} type="file" accept="image/png,image/jpeg,image/webp,image/gif" hidden data-testid="icon-upload" onChange={(e) => upload(e.target.files?.[0])} />
                  </>
                )}
                {value && (
                  <button type="button" className="ml-auto rounded-md px-2 py-1 text-[13px] text-muted hover:bg-surface-2" onClick={() => pick("")}>
                    No icon
                  </button>
                )}
              </div>
            )}
            {err && <p className="px-3 pb-2 text-xs text-negative">{err}</p>}
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
    </div>
  );
}
