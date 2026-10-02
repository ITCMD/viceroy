import { BarChart3, ChevronLeft, ChevronRight } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { Button, CategoryIcon, EmptyState, MoneyText } from "@/components/ui";
import { formatMoney } from "@/lib/format";
import { bucketLabel, type Line, type Report } from "./api";

export function useLabels(report: Report) {
  return useMemo(() => {
    const multiYear = report.buckets.length > 0 && report.buckets[0].start.slice(0, 4) !== report.buckets.at(-1)!.start.slice(0, 4);
    return report.buckets.map((b) => bucketLabel(b, report.interval, multiYear));
  }, [report]);
}

export const pct = (part: number, whole: number) => (whole > 0 ? `${((part / whole) * 100).toFixed(part / whole < 0.1 ? 1 : 0)}%` : "—");

/** Cents as dollars for the Discuss context. */
export const dollars = (cents: number) => Math.round(cents) / 100;

/** Bars fill more of the chart when there are only a few periods. */
export const barWidthFor = (buckets: number) => (buckets <= 1 ? 140 : buckets <= 3 ? 110 : buckets <= 6 ? 76 : buckets <= 12 ? 40 : 28);

export function NoData() {
  return (
    <EmptyState icon={BarChart3} title="No transactions in this range">
      Try a longer date range, or step back with the arrows.
    </EmptyState>
  );
}

/** ‹ Sep 2026 › — steps a report's date window back and forward. */
export function PeriodNav({
  label,
  onPrev,
  onNext,
  onToday,
  atStart,
  atEnd,
}: {
  label: string;
  onPrev: () => void;
  onNext: () => void;
  onToday: () => void;
  atStart?: boolean;
  atEnd: boolean;
}) {
  return (
    <div className="flex items-center gap-1" data-testid="report-period">
      <Button variant="ghost" size="sm" aria-label="Previous period" onClick={onPrev} disabled={atStart}>
        <ChevronLeft size={16} />
      </Button>
      <h2 className="min-w-36 text-center text-[15px] font-semibold" data-testid="report-period-label">
        {label}
      </h2>
      <Button variant="ghost" size="sm" aria-label="Next period" onClick={onNext} disabled={atEnd}>
        <ChevronRight size={16} />
      </Button>
      {!atEnd && (
        <Button variant="secondary" size="sm" onClick={onToday}>
          Today
        </Button>
      )}
    </div>
  );
}

/** Table view of a breakdown: every line with its total, share and average; doubles as the chart legend. */
export function BreakdownTable({
  lines,
  total,
  buckets,
  colors,
  otherColor,
  per,
}: {
  lines: Line[];
  total: number;
  buckets: number;
  colors?: Map<string, string>;
  otherColor?: string;
  per?: string;
}) {
  if (lines.length === 0) return <p className="py-4 text-center text-sm text-muted">Nothing in this range.</p>;
  return (
    <table className="w-full text-[13px]" data-testid="breakdown-table">
      <thead>
        <tr className="text-left text-xs text-muted">
          <th className="pb-2 font-medium">Name</th>
          {per && <th className="hidden pb-2 text-right font-medium sm:table-cell">Avg / {per}</th>}
          <th className="pb-2 text-right font-medium">Share</th>
          <th className="pb-2 text-right font-medium">Total</th>
        </tr>
      </thead>
      <tbody className="divide-y divide-border">
        {lines.map((l) => {
          const color = colors ? (colors.get(l.key) ?? otherColor) : undefined;
          const share = total > 0 ? Math.max(0, l.total / total) : 0;
          return (
            <tr key={l.key}>
              <td className="py-2 pr-2">
                <div className="flex min-w-0 items-center gap-2">
                  {color && <span aria-hidden className="size-2 shrink-0 rounded-sm" style={{ background: color }} />}
                  {l.icon && <CategoryIcon icon={l.icon} size="sm" />}
                  <div className="min-w-0 flex-1">
                    <div className="truncate">{l.name}</div>
                    <div className="mt-1 h-1 rounded-full bg-surface-2">
                      <div className="h-1 rounded-full bg-muted/50" style={{ width: `${share * 100}%` }} />
                    </div>
                  </div>
                </div>
              </td>
              {per && (
                <td className="hidden py-2 text-right text-muted tabular sm:table-cell">{formatMoney(Math.round(l.total / Math.max(buckets, 1)), { whole: true })}</td>
              )}
              <td className="py-2 text-right text-muted tabular">{pct(l.total, total)}</td>
              <td className="py-2 pl-2 text-right font-medium">
                <MoneyText cents={l.total} />
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

/** useState kept in sessionStorage, so a report looks the same after visiting its transactions and coming back. */
export function useSessionState<T>(key: string, initial: T): [T, (v: T | ((prev: T) => T)) => void] {
  const [v, setV] = useState<T>(() => {
    try {
      const raw = sessionStorage.getItem(key);
      if (raw) return JSON.parse(raw) as T;
    } catch {
      /* storage unavailable */
    }
    return initial;
  });
  const set = useCallback(
    (next: T | ((prev: T) => T)) =>
      setV((prev) => {
        const val = typeof next === "function" ? (next as (p: T) => T)(prev) : next;
        try {
          sessionStorage.setItem(key, JSON.stringify(val));
        } catch {
          /* ignore */
        }
        return val;
      }),
    [key],
  );
  return [v, set];
}
