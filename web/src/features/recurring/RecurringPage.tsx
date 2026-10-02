import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { Check, ChevronLeft, ChevronRight, Plus, Repeat, Sparkles } from "lucide-react";
import { useMemo, useState } from "react";
import { Button, Card, CategoryIcon, EmptyState, MoneyText, PageHeader } from "@/components/ui";
import { cadenceLabels, dueLabel, recurringQuery, useDismissRecurring, type Occurrence, type RecurringSeries } from "./api";
import { RecurringItemDialog, type RecurringDraft } from "./RecurringItemDialog";

const iso = (d: Date) => d.toISOString().slice(0, 10);
const monthStart = (d: string) => d.slice(0, 8) + "01";
const shortDay = (d: string) => new Date(d + "T00:00:00").toLocaleDateString("en-US", { weekday: "short", month: "short", day: "numeric" });

function monthRange(start: string) {
  const [y, m] = start.split("-").map(Number);
  return { from: start, to: iso(new Date(Date.UTC(y, m, 0))) };
}

function addMonths(start: string, n: number) {
  const [y, m] = start.split("-").map(Number);
  return iso(new Date(Date.UTC(y, m - 1 + n, 1)));
}

/** Recurring: what's due before the next paycheck, a month calendar, suggestions and every
 * tracked item. */
export function RecurringPage() {
  const { data: now } = useQuery(recurringQuery());
  const [month, setMonth] = useState<string | null>(null);
  const shown = month ?? (now ? monthStart(now.today) : null);
  const range = shown ? monthRange(shown) : { from: "", to: "" };
  const { data: cal } = useQuery({ ...recurringQuery(range.from, range.to), enabled: !!shown && shown !== (now && monthStart(now.today)) });
  const calData = shown && now && shown === monthStart(now.today) ? now : cal;
  const [draft, setDraft] = useState<RecurringDraft | null>(null);
  const [showDismissed, setShowDismissed] = useState(false);
  const dismiss = useDismissRecurring();

  const open = (o: { item_id: number; key: string; source: string }) => {
    if (!now) return;
    const s = o.source === "tracked" ? now.tracked.find((x) => x.id === o.item_id) : now.suggestions.find((x) => x.key === o.key);
    if (s) setDraft({ kind: o.source === "tracked" ? "item" : "suggestion", s });
  };

  return (
    <>
      <PageHeader
        title="Recurring"
        actions={
          <Button size="sm" onClick={() => setDraft({ kind: "new" })}>
            <Plus size={15} /> Add recurring
          </Button>
        }
      />
      <div className="mx-auto flex max-w-5xl flex-col gap-4 p-4 md:p-6">
        {now && <BeforePayday data={now} onOpen={open} />}
        {shown && (
          <Card
            title={new Date(shown + "T00:00:00").toLocaleDateString("en-US", { month: "long", year: "numeric" })}
            action={
              <div className="flex items-center gap-1">
                <Button size="sm" variant="ghost" aria-label="Previous month" onClick={() => setMonth(addMonths(shown, -1))}>
                  <ChevronLeft size={16} />
                </Button>
                {now && shown !== monthStart(now.today) && (
                  <Button size="sm" variant="ghost" onClick={() => setMonth(null)}>
                    Today
                  </Button>
                )}
                <Button size="sm" variant="ghost" aria-label="Next month" onClick={() => setMonth(addMonths(shown, 1))}>
                  <ChevronRight size={16} />
                </Button>
              </div>
            }
          >
            <Calendar month={shown} today={now?.today ?? ""} payday={now?.next_payday ?? ""} occurrences={calData?.occurrences ?? []} onOpen={open} />
          </Card>
        )}
        {now && now.suggestions.length > 0 && (
          <Card title={<span className="flex items-center gap-1.5"><Sparkles size={15} className="text-accent" /> Looks recurring</span>}>
            <p className="-mt-1 mb-2 text-[13px] text-muted">Viceroy spotted these repeating. Track the ones that are real so they show up as upcoming and can be edited.</p>
            <ul className="-mx-1 divide-y divide-border" data-testid="recurring-suggestions">
              {now.suggestions.map((s) => (
                <SeriesRow key={s.key} s={s} today={now.today} onClick={() => setDraft({ kind: "suggestion", s })}>
                  <Button size="sm" variant="secondary" onClick={() => setDraft({ kind: "suggestion", s })}>
                    Track
                  </Button>
                  <Button size="sm" variant="ghost" disabled={dismiss.isPending} onClick={() => dismiss.mutate({ key: s.key, dismissed: true })}>
                    Not recurring
                  </Button>
                </SeriesRow>
              ))}
            </ul>
          </Card>
        )}
        <Card title="All recurring">
          {now && now.tracked.length === 0 ? (
            <EmptyState icon={Repeat} title="Nothing tracked yet">
              Track a suggestion above, use “Mark as recurring” on a transaction, or add one by hand.
            </EmptyState>
          ) : (
            <ul className="-mx-1 -my-1 divide-y divide-border" data-testid="recurring-list">
              {now?.tracked.map((s) => (
                <SeriesRow key={s.key} s={s} today={now.today} onClick={() => setDraft({ kind: "item", s })} />
              ))}
            </ul>
          )}
          {now && now.dismissed.length > 0 && (
            <div className="mt-3 border-t border-border pt-3">
              <button className="text-xs font-medium text-muted hover:text-text" onClick={() => setShowDismissed((v) => !v)}>
                {showDismissed ? "Hide" : "Show"} {now.dismissed.length} marked not recurring
              </button>
              {showDismissed && (
                <ul className="-mx-1 mt-1 divide-y divide-border opacity-70">
                  {now.dismissed.map((s) => (
                    <SeriesRow key={s.key} s={s} today={now.today}>
                      <Button size="sm" variant="ghost" disabled={dismiss.isPending} onClick={() => dismiss.mutate({ key: s.key, dismissed: false })}>
                        Restore
                      </Button>
                    </SeriesRow>
                  ))}
                </ul>
              )}
            </div>
          )}
        </Card>
      </div>
      <RecurringItemDialog draft={draft} onClose={() => setDraft(null)} />
    </>
  );
}

function BeforePayday({ data, onOpen }: { data: { today: string; next_payday: string; before_payday: Occurrence[] }; onOpen: (o: Occurrence) => void }) {
  const out = data.before_payday.filter((o) => o.amount < 0).reduce((a, o) => a - o.amount, 0);
  return (
    <Card
      title={`Before your next paycheck · ${new Date(data.next_payday + "T00:00:00").toLocaleDateString("en-US", { month: "short", day: "numeric" })}`}
      action={out > 0 ? <span className="text-[13px] text-muted">
        <MoneyText cents={out} /> going out
      </span> : undefined}
    >
      {data.before_payday.length === 0 ? (
        <p className="text-sm text-muted">Nothing recurring is due before then.</p>
      ) : (
        <ul className="-mx-1 -my-1 divide-y divide-border" data-testid="before-payday">
          {data.before_payday.map((o) => (
            <li key={o.key + o.date}>
              <button className="flex w-full items-center gap-3 px-1 py-2 text-left hover:bg-surface-2" onClick={() => onOpen(o)}>
                <CategoryIcon icon={o.category_icon} />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{o.name}</div>
                  <div className="text-xs text-muted">{cadenceLabels[o.cadence]}{o.source === "detected" && " · not confirmed"}</div>
                </div>
                <div className="text-right">
                  <MoneyText cents={o.amount} colored className="text-sm" />
                  <div className={clsx("text-xs", o.status === "due" ? "text-negative" : "text-muted")}>{dueLabel(o.date, data.today)}</div>
                </div>
              </button>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function SeriesRow({ s, today, onClick, children }: { s: RecurringSeries; today: string; onClick?: () => void; children?: React.ReactNode }) {
  return (
    <li className="flex items-center gap-2 px-1 py-2" data-testid="recurring-row">
      <button className="flex min-w-0 flex-1 items-center gap-3 rounded-md text-left disabled:cursor-default" disabled={!onClick} onClick={onClick}>
        <CategoryIcon icon={s.category_icon} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{s.name}</div>
          <div className="truncate text-xs text-muted">
            {cadenceLabels[s.cadence]}
            {s.account_name && ` · ${s.account_name}`}
            {s.source === "detected" && ` · seen ${s.count}×`}
          </div>
        </div>
        <div className="text-right">
          <div className="text-sm">
            {s.variable && <span className="text-muted">~</span>}
            <MoneyText cents={s.amount} colored />
          </div>
          {s.next_date && <div className={clsx("text-xs", s.next_date < today ? "text-negative" : "text-muted")}>{dueLabel(s.next_date, today)}</div>}
        </div>
      </button>
      {children}
    </li>
  );
}

const weekdayNames = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

function Calendar({ month, today, payday, occurrences, onOpen }: { month: string; today: string; payday: string; occurrences: Occurrence[]; onOpen: (o: Occurrence) => void }) {
  const cells = useMemo(() => {
    const [y, m] = month.split("-").map(Number);
    const first = new Date(Date.UTC(y, m - 1, 1));
    const n = new Date(Date.UTC(y, m, 0)).getUTCDate();
    const lead = first.getUTCDay();
    const out: (string | null)[] = Array(lead).fill(null);
    for (let d = 1; d <= n; d++) out.push(iso(new Date(Date.UTC(y, m - 1, d))));
    while (out.length % 7) out.push(null);
    return out;
  }, [month]);
  const byDay = useMemo(() => {
    const m = new Map<string, Occurrence[]>();
    for (const o of occurrences) m.set(o.date, [...(m.get(o.date) ?? []), o]);
    return m;
  }, [occurrences]);
  const [picked, setPicked] = useState<string | null>(null);
  const pickedList = picked ? byDay.get(picked) ?? [] : [];

  return (
    <div data-testid="recurring-calendar">
      <div className="grid grid-cols-7 border-b border-border pb-1 text-center text-[11px] font-semibold uppercase tracking-wide text-muted">
        {weekdayNames.map((d) => (
          <div key={d}>{d}</div>
        ))}
      </div>
      <div className="grid grid-cols-7">
        {cells.map((d, i) =>
          d ? (
            <div
              key={d}
              className={clsx(
                "min-h-16 border-b border-r border-border p-1 md:min-h-24",
                i % 7 === 0 && "border-l",
                d === picked && "bg-surface-2",
              )}
              onClick={() => setPicked(d === picked ? null : d)}
              data-testid={`cal-${d}`}
            >
              <div className="flex items-center justify-between">
                <span className={clsx("grid size-5 place-items-center rounded-full text-[11px]", d === today ? "bg-accent font-semibold text-accent-fg" : "text-muted")}>{Number(d.slice(8))}</span>
                {d === payday && <span className="text-[10px] font-medium text-positive" title="Next payday">Payday</span>}
              </div>
              <div className="mt-0.5 flex flex-col gap-0.5">
                {(byDay.get(d) ?? []).slice(0, 3).map((o) => (
                  <Chip key={o.key} o={o} onOpen={onOpen} />
                ))}
                {(byDay.get(d)?.length ?? 0) > 3 && <span className="px-1 text-[10px] text-muted">+{byDay.get(d)!.length - 3} more</span>}
              </div>
            </div>
          ) : (
            <div key={`x${i}`} className={clsx("border-b border-r border-border bg-surface-2/40", i % 7 === 0 && "border-l")} />
          ),
        )}
      </div>
      {picked && (
        <div className="mt-3 md:hidden">
          <div className="text-[13px] font-medium">{shortDay(picked)}</div>
          {pickedList.length === 0 ? (
            <p className="text-[13px] text-muted">Nothing due.</p>
          ) : (
            <ul className="divide-y divide-border">
              {pickedList.map((o) => (
                <li key={o.key}>
                  <button className="flex w-full items-center gap-2 py-2 text-left text-sm" onClick={() => onOpen(o)}>
                    <span aria-hidden>{o.category_icon || "•"}</span>
                    <span className="flex-1 truncate">{o.name}</span>
                    <StatusText o={o} />
                    <MoneyText cents={o.amount} colored />
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
      <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
        <span className="flex items-center gap-1"><span className="size-2 rounded-full bg-upcoming" /> Upcoming</span>
        <span className="flex items-center gap-1"><Check size={12} /> Paid</span>
        <span className="flex items-center gap-1"><span className="size-2 rounded-full bg-negative" /> Late or missed</span>
        <span className="flex items-center gap-1"><span className="size-2 rounded-full border border-dashed border-muted" /> Not confirmed</span>
      </div>
    </div>
  );
}

function StatusText({ o }: { o: Occurrence }) {
  if (o.status === "paid") return <span className="text-xs text-muted">Paid</span>;
  if (o.status === "due") return <span className="text-xs text-negative">Late</span>;
  if (o.status === "missed") return <span className="text-xs text-negative">Missed</span>;
  return null;
}

function Chip({ o, onOpen }: { o: Occurrence; onOpen: (o: Occurrence) => void }) {
  return (
    <button
      type="button"
      title={`${o.name} · ${o.status === "paid" ? "paid" : o.status === "due" ? "late" : o.status}`}
      onClick={(e) => {
        e.stopPropagation();
        onOpen(o);
      }}
      className={clsx(
        "flex w-full items-center gap-1 truncate rounded px-1 py-px text-left text-[11px] leading-4",
        o.source === "detected" ? "border border-dashed border-border" : "bg-surface-2",
        o.status === "paid" && "text-muted",
        (o.status === "due" || o.status === "missed") && "text-negative",
        o.status === "upcoming" && "text-upcoming",
      )}
      data-testid="cal-chip"
    >
      {o.status === "paid" ? <Check size={10} className="shrink-0" /> : <span aria-hidden className="shrink-0">{o.category_icon || "•"}</span>}
      <span className="hidden truncate md:inline">{o.name}</span>
      <MoneyText cents={Math.abs(o.amount)} whole className="ml-auto hidden shrink-0 lg:inline" />
    </button>
  );
}
