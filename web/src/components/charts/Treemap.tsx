import { Glyph } from "@/components/ui";
import clsx from "clsx";
import { useEffect, useMemo, useRef, useState } from "react";
import { inkFor } from "@/lib/color";
import { formatMoney } from "@/lib/format";

export type TreemapItem = { key: string; label: string; icon?: string; value: number; color: string };

type Rect = { x: number; y: number; w: number; h: number };

/** Squarified treemap layout: rectangles for items (largest first) filling a w×h box. */
export function squarify(values: number[], box: Rect): Rect[] {
  const total = values.reduce((a, v) => a + v, 0);
  const out: Rect[] = new Array(values.length);
  if (total <= 0 || box.w <= 0 || box.h <= 0) return values.map(() => ({ x: 0, y: 0, w: 0, h: 0 }));
  const scale = (box.w * box.h) / total;
  let rect = { ...box };
  let i = 0;
  const worst = (row: number[], side: number) => {
    const s = row.reduce((a, v) => a + v, 0);
    const max = Math.max(...row);
    const min = Math.min(...row);
    return Math.max((side * side * max) / (s * s), (s * s) / (side * side * min));
  };
  while (i < values.length) {
    const side = Math.min(rect.w, rect.h);
    const row = [values[i] * scale];
    let j = i + 1;
    while (j < values.length) {
      const next = [...row, values[j] * scale];
      if (worst(next, side) > worst(row, side)) break;
      row.push(values[j] * scale);
      j++;
    }
    const area = row.reduce((a, v) => a + v, 0);
    const horizontal = rect.w >= rect.h; // lay the row down the short side
    const thick = area / side;
    let pos = 0;
    row.forEach((a, k) => {
      const len = a / thick;
      out[i + k] = horizontal ? { x: rect.x, y: rect.y + pos, w: thick, h: len } : { x: rect.x + pos, y: rect.y, w: len, h: thick };
      pos += len;
    });
    rect = horizontal ? { x: rect.x + thick, y: rect.y, w: rect.w - thick, h: rect.h } : { x: rect.x, y: rect.y + thick, w: rect.w, h: rect.h - thick };
    i = j;
  }
  return out;
}

/**
 * Space-filling boxes sized by amount (cents), like a disk-usage map for money. Items with
 * no positive value are left out. Clicking a box calls onSelect.
 */
export function Treemap({
  items,
  total,
  height = 420,
  label,
  onSelect,
  selectHint,
}: {
  items: TreemapItem[];
  /** What percentages are of; defaults to the sum of the items. */
  total?: number;
  height?: number;
  label: string;
  onSelect?: (key: string) => void;
  /** Accessible hint for what clicking a box does, e.g. "Show categories". */
  selectHint?: string;
}) {
  const el = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  useEffect(() => {
    if (!el.current) return;
    const ro = new ResizeObserver(([e]) => setWidth(e.contentRect.width));
    ro.observe(el.current);
    return () => ro.disconnect();
  }, []);
  const shown = useMemo(() => items.filter((i) => i.value > 0).sort((a, b) => b.value - a.value), [items]);
  const sum = total ?? shown.reduce((a, i) => a + i.value, 0);
  const rects = useMemo(() => squarify(shown.map((i) => i.value), { x: 0, y: 0, w: width, h: height }), [shown, width, height]);

  return (
    <div ref={el} className="relative w-full overflow-hidden rounded-lg" style={{ height }} role="list" aria-label={label} data-testid="treemap">
      {width > 0 &&
        shown.map((it, k) => {
          const r = rects[k];
          const roomy = r.w > 72 && r.h > 44;
          const tiny = r.w < 28 || r.h < 18;
          const share = sum > 0 ? (it.value / sum) * 100 : 0;
          const ink = inkFor(it.color);
          return (
            <button
              key={it.key}
              type="button"
              role="listitem"
              onClick={() => onSelect?.(it.key)}
              title={`${it.label}: ${formatMoney(it.value)} (${share.toFixed(1)}%)`}
              aria-label={`${it.label}, ${formatMoney(it.value)}, ${share.toFixed(1)}%${selectHint ? `. ${selectHint}` : ""}`}
              className={clsx(
                "group absolute overflow-hidden text-left transition-[filter] hover:brightness-110 focus-visible:z-10 focus-visible:outline-2 focus-visible:outline-text",
                !onSelect && "cursor-default",
              )}
              style={{ left: r.x, top: r.y, width: r.w, height: r.h, padding: 1 }}
            >
              <span
                className="flex h-full w-full flex-col justify-between rounded-md p-2"
                style={{ background: it.color, color: ink }}
              >
                {!tiny && (
                  <span className="min-w-0">
                    <span className={clsx("block truncate font-medium leading-tight", roomy ? "text-[13px]" : "text-[11px]")}>
                      {it.icon && <Glyph icon={it.icon} className="mr-1" />}
                      {it.label}
                    </span>
                    {r.h > 34 && <span className="block truncate text-[11px] leading-tight opacity-90">{formatMoney(it.value, { whole: true })}</span>}
                  </span>
                )}
                {roomy && <span className="self-end text-lg font-semibold leading-none tabular opacity-95">{share < 10 ? share.toFixed(1) : Math.round(share)}%</span>}
              </span>
            </button>
          );
        })}
    </div>
  );
}
