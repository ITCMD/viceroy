import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { useState } from "react";
import { Button, CategoryIcon, Dialog, MoneyText } from "@/components/ui";
import { cadenceLabels, dueLabel, recurringQuery, useDismissRecurring, type RecurringSeries } from "./api";

/** Every detected recurring series, with dismiss/restore. Opened from the dashboard widget. */
export function RecurringDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const { data } = useQuery(recurringQuery);
  const [showDismissed, setShowDismissed] = useState(false);
  const active = data?.series.filter((s) => !s.dismissed) ?? [];
  const dismissed = data?.series.filter((s) => s.dismissed) ?? [];
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Recurring" description="Detected from repeating charges and deposits." className="max-w-lg">
      {active.length === 0 ? (
        <p className="py-6 text-center text-sm text-muted">Nothing recurring detected yet. It takes a few months of history.</p>
      ) : (
        <ul className="-mx-1 divide-y divide-border" data-testid="recurring-list">
          {active.map((s) => (
            <SeriesRow key={s.key} s={s} today={data!.today} />
          ))}
        </ul>
      )}
      {dismissed.length > 0 && (
        <div className="mt-3 border-t border-border pt-3">
          <button className="text-xs font-medium text-muted hover:text-text" onClick={() => setShowDismissed((v) => !v)}>
            {showDismissed ? "Hide" : "Show"} {dismissed.length} dismissed
          </button>
          {showDismissed && (
            <ul className="-mx-1 mt-1 divide-y divide-border">
              {dismissed.map((s) => (
                <SeriesRow key={s.key} s={s} today={data!.today} />
              ))}
            </ul>
          )}
        </div>
      )}
    </Dialog>
  );
}

function SeriesRow({ s, today }: { s: RecurringSeries; today: string }) {
  const dismiss = useDismissRecurring();
  return (
    <li className={clsx("flex items-center gap-3 px-1 py-2.5", s.dismissed && "opacity-60")} data-testid="recurring-row">
      <CategoryIcon icon={s.category_icon} />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{s.name}</div>
        <div className="truncate text-xs text-muted">
          {cadenceLabels[s.cadence]} · {s.account_name}
        </div>
      </div>
      <div className="text-right">
        <div className="text-sm">
          {s.variable && <span className="text-muted">~</span>}
          <MoneyText cents={s.amount} colored />
        </div>
        <div className={clsx("text-xs", s.next_date < today ? "text-negative" : "text-muted")}>{dueLabel(s.next_date, today)}</div>
      </div>
      <Button variant="ghost" size="sm" disabled={dismiss.isPending} onClick={() => dismiss.mutate({ key: s.key, dismissed: !s.dismissed })}>
        {s.dismissed ? "Restore" : "Dismiss"}
      </Button>
    </li>
  );
}
