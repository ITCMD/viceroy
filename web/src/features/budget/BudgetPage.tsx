import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import clsx from "clsx";
import { ChevronDown, ChevronLeft, ChevronRight, Download, Eye, EyeOff, Sparkles, Target as TargetIcon, Upload } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { Button, Card, CategoryIcon, MoneyText, PageHeader, Segmented } from "@/components/ui";
import { AccountAvatar } from "@/features/accounts/AccountAvatar";
import { accountsQuery } from "@/features/accounts/api";
import { ChatSheet } from "@/features/chat/ChatSheet";
import { CloseoutBanner } from "@/features/closeout/CloseoutBanner";
import type { ChatContext } from "@/features/chat/api";
import { reportQuery, type Report } from "@/features/reports/api";
import { dollars } from "@/features/reports/shared";
import { formatMoney } from "@/lib/format";
import { BudgetEditDialog } from "./BudgetEditDialog";
import { BudgetImportDialog } from "./BudgetImportDialog";
import { budgetQuery, chunkLabel, periodLabel, remaining, type Budget, type BudgetGroup, type BudgetLine, type Target, type View } from "./api";

const views: { value: View; label: string }[] = [
  { value: "month", label: "Month" },
  { value: "week", label: "Week" },
  { value: "paycheck", label: "Paycheck" },
];

const VIEW_KEY = "viceroy.budget.view";
const OPEN_KEY = "viceroy.budget.open"; // ids of split lines (Debt Repayment) shown expanded

function loadOpen(): number[] {
  try {
    const v = JSON.parse(localStorage.getItem(OPEN_KEY) ?? "[]");
    return Array.isArray(v) ? v.filter((x) => typeof x === "number") : [];
  } catch {
    return [];
  }
}

function initialView(): View {
  try {
    const v = localStorage.getItem(VIEW_KEY);
    if (v === "week" || v === "paycheck") return v;
  } catch {
    /* storage unavailable */
  }
  return "month";
}

const cols = "grid grid-cols-[minmax(0,1fr)_5.5rem_5.5rem] items-center gap-2 sm:grid-cols-[minmax(0,1fr)_6rem_6rem_6rem]";

export function BudgetPage() {
  const [view, setViewState] = useState<View>(initialView);
  const [date, setDate] = useState("");
  const [editing, setEditing] = useState<{ target: Target; line: BudgetLine } | null>(null);
  const [importing, setImporting] = useState(false);
  const [showHidden, setShowHidden] = useState(false);
  const [open, setOpenState] = useState<number[]>(loadOpen);
  const [chatting, setChatting] = useState(false);
  const { data: b } = useQuery(budgetQuery(view, date));
  // The six months before the one shown, for the chat: what each category really cost.
  const past = b ? pastMonths(b.month, 6) : null;
  const { data: history } = useQuery({ ...reportQuery(past?.from ?? "", "month", "category", past?.to ?? ""), enabled: !!past });
  const chatContext = useMemo(() => (b ? budgetContext(b, history) : undefined), [b, history]);
  const toggleOpen = (id: number) => {
    const next = open.includes(id) ? open.filter((x) => x !== id) : [...open, id];
    setOpenState(next);
    try {
      localStorage.setItem(OPEN_KEY, JSON.stringify(next));
    } catch {
      /* ignore */
    }
  };

  const setView = (v: View) => {
    setViewState(v);
    try {
      localStorage.setItem(VIEW_KEY, v);
    } catch {
      /* ignore */
    }
  };
  const isCurrent = b ? b.today >= b.start && b.today <= b.end : true;
  // Hidden categories stay out of the way unless they have activity this period.
  const hiddenCount = b?.groups.reduce((n, g) => n + g.lines.filter((l) => l.hidden && l.actual === 0).length, 0) ?? 0;

  return (
    <>
      <PageHeader
        title="Budget"
        actions={
          <>
            <Button variant="secondary" size="sm" onClick={() => setChatting(true)} disabled={!chatContext} data-testid="budget-chat">
              <Sparkles size={14} /> Chat
            </Button>
            <Button variant="secondary" size="sm" onClick={() => setImporting(true)} disabled={!b}>
              <Upload size={14} /> Import
            </Button>
            <a
              href={b ? `/api/budget/export?month=${b.month}` : undefined}
              download
              className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border bg-surface px-3 text-[13px] font-medium text-text transition hover:bg-surface-2"
              aria-label="Export budget as CSV"
            >
              <Download size={14} />
              <span className="hidden sm:inline">Export</span>
            </a>
          </>
        }
      />
      <div className="mx-auto flex max-w-6xl flex-col gap-4 p-4 md:p-6">
        <CloseoutBanner showClosed />
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-1">
            <Button variant="ghost" size="sm" aria-label="Previous period" onClick={() => b && setDate(b.prev)}>
              <ChevronLeft size={16} />
            </Button>
            <h2 className="min-w-40 text-center text-[15px] font-semibold" data-testid="budget-period">
              {b ? periodLabel(b) : " "}
            </h2>
            <Button variant="ghost" size="sm" aria-label="Next period" onClick={() => b && setDate(b.next)}>
              <ChevronRight size={16} />
            </Button>
            {!isCurrent && (
              <Button variant="secondary" size="sm" onClick={() => setDate("")}>
                Today
              </Button>
            )}
          </div>
          <Segmented label="Budget view" value={view} onChange={setView} items={views} />
        </div>
        {b && view !== "month" && (
          <p className="-mt-2 text-[13px] text-muted">
            Each amount is this {view === "week" ? "week" : "paycheck"}'s share of the monthly budget, based on what's left this month and when it's usually spent.
          </p>
        )}

        {b && (
          <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
            <div className="order-2 flex flex-col gap-4 lg:order-1">
              {b.groups.map((g) => (
                <GroupCard
                  key={`${g.kind}-${g.id}`}
                  g={g}
                  period={b}
                  showPacing={isCurrent}
                  showHidden={showHidden}
                  open={open}
                  onToggle={toggleOpen}
                  onEdit={(line, target) => setEditing({ target: target ?? { kind: g.kind === "goals" ? "goal" : "category", id: line.id }, line })}
                />
              ))}
              {(hiddenCount > 0 || showHidden) && (
                <Button variant="ghost" size="sm" className="self-start text-muted" onClick={() => setShowHidden(!showHidden)}>
                  {showHidden ? <EyeOff size={14} /> : <Eye size={14} />}
                  {showHidden ? "Hide hidden categories" : `Show ${hiddenCount} hidden ${hiddenCount === 1 ? "category" : "categories"}`}
                </Button>
              )}
            </div>
            <Summary b={b} className="order-1 lg:sticky lg:top-18 lg:order-2" />
          </div>
        )}
      </div>
      <BudgetEditDialog
        target={editing?.target ?? null}
        line={editing?.line ?? null}
        month={b?.month ?? ""}
        period={b ? { start: b.start, end: b.end } : null}
        forwardDefault={b?.settings.forward_default ?? false}
        onClose={() => setEditing(null)}
      />
      <BudgetImportDialog open={importing} onOpenChange={setImporting} month={b?.month ?? ""} />
      <ChatSheet open={chatting} onOpenChange={setChatting} context={chatContext} />
    </>
  );
}

type Period = { start: string; end: string };

/** Transactions behind a budget line: its category (or goal) within the period shown. */
function lineTransactions(kind: "category" | "goal", id: number, period: Period) {
  return { [kind]: id, from: period.start, to: period.end };
}

function GroupCard({
  g,
  period,
  showPacing,
  showHidden,
  open,
  onToggle,
  onEdit,
}: {
  g: BudgetGroup;
  period: Period;
  showPacing: boolean;
  showHidden: boolean;
  open: number[];
  onToggle: (id: number) => void;
  onEdit: (l: BudgetLine, t?: Target) => void;
}) {
  const income = g.kind === "income";
  const accounts = useQuery(accountsQuery).data?.accounts ?? [];
  const lines = g.lines.filter((l) => showHidden || !l.hidden || l.actual !== 0);
  if (lines.length === 0 && g.lines.length > 0) return null;
  return (
    <section className="overflow-hidden rounded-xl border border-border bg-surface" data-testid={`budget-group-${g.kind}`}>
      <header className={clsx(cols, "border-b border-border px-4 py-2.5 text-[13px]")}>
        <h3 className="text-[15px] font-semibold">
          {g.name}
          {g.kind === "non_monthly" && <span className="ml-2 text-xs font-normal text-muted">Rolls over monthly</span>}
        </h3>
        <span className="hidden text-right text-muted sm:block">Budget</span>
        <span className="text-right text-muted">{income ? "Received" : "Actual"}</span>
        <span className="text-right text-muted">Remaining</span>
      </header>
      {g.lines.length === 0 && g.kind === "goals" ? (
        <div className="flex items-center gap-3 px-4 py-3 text-[13px] text-muted">
          <TargetIcon size={16} className="text-accent" />
          <span>
            Set aside money each month toward something.{" "}
            <Link to={"/goals" as string} className="font-medium text-accent hover:underline">
              Create a goal
            </Link>
          </span>
        </div>
      ) : (
        <ul className="divide-y divide-border">
          {lines.map((l) => {
            const subs = l.lines ?? [];
            const expanded = open.includes(l.id);
            return (
              <li key={l.id}>
                <LineRow
                  l={l}
                  income={income}
                  showPacing={showPacing}
                  onClick={() => (subs.length ? onToggle(l.id) : onEdit(l))}
                  expanded={subs.length ? expanded : undefined}
                  txns={lineTransactions(g.kind === "goals" ? "goal" : "category", l.id, period)}
                />
                {subs.length > 0 && expanded && (
                  <ul className="divide-y divide-border border-t border-border bg-surface-2/30" data-testid="budget-sublines">
                    {subs.map((sl) => {
                      const acct = sl.account_id ? accounts.find((a) => a.id === sl.account_id) : undefined;
                      return (
                        <li key={`${sl.account_id ?? 0}-${sl.id}`}>
                          <LineRow
                            l={sl}
                            income={false}
                            showPacing={showPacing}
                            indent
                            leading={
                              sl.account_id ? (
                                <AccountAvatar account={acct ?? { name: sl.name, institution_name: "", color: "", logo_url: null }} size={20} />
                              ) : undefined
                            }
                            onClick={() =>
                              onEdit(sl, sl.account_id ? { kind: "account", id: sl.account_id, categoryId: l.id } : { kind: "category", id: l.id, other: true })
                            }
                            txns={sl.account_id ? { account: sl.account_id, from: period.start, to: period.end } : lineTransactions("category", l.id, period)}
                          />
                        </li>
                      );
                    })}
                  </ul>
                )}
              </li>
            );
          })}
        </ul>
      )}
      <footer className={clsx(cols, "border-t border-border bg-surface-2/50 px-4 py-2 text-[13px] font-semibold")}>
        <span>Total {g.name.toLowerCase()}</span>
        <MoneyText cents={g.budget} className="hidden text-right sm:block" />
        <MoneyText cents={g.actual} className="text-right" />
        <Remaining cents={remaining(g)} income={income} />
      </footer>
    </section>
  );
}

function LineRow({
  l,
  income,
  showPacing,
  onClick,
  txns,
  expanded,
  indent,
  leading,
}: {
  l: BudgetLine;
  income: boolean;
  showPacing: boolean;
  onClick: () => void;
  txns: Record<string, string | number>;
  /** Set on a line with sub-lines: clicking it opens or closes them. */
  expanded?: boolean;
  indent?: boolean;
  /** Replaces the category icon (an account avatar on debt lines). */
  leading?: ReactNode;
}) {
  const pct = l.budget > 0 ? Math.min(1, l.actual / l.budget) : l.actual > 0 ? 1 : 0;
  const over = !income && l.actual > l.budget;
  const ahead = !income && showPacing && l.expected > 0 && l.actual > l.expected && !over;
  const timing = [chunkLabel(l.chunk), l.chunk.no_pacing && "Not paced"].filter(Boolean).join(" · ");
  // Recurring charges still to come in the upcoming window, drawn after the spent fill.
  const soon = income ? 0 : l.upcoming;
  const soonPct = soon > 0 ? (l.budget > 0 ? Math.min(1 - pct, soon / l.budget) : 1 - pct) : 0;
  return (
    // A div rather than a button so the Actual amount can be a link of its own.
    <div
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(e) => {
        if (e.target === e.currentTarget && (e.key === "Enter" || e.key === " ")) {
          e.preventDefault();
          onClick();
        }
      }}
      className={clsx(
        cols,
        "w-full cursor-pointer py-2 pr-4 text-left text-[13px] outline-none hover:bg-surface-2 focus-visible:bg-surface-2",
        indent ? "pl-11" : "pl-4",
        l.hidden && "opacity-60",
      )}
      aria-expanded={expanded}
      data-testid={indent ? "budget-subline" : "budget-line"}
    >
      <span className="flex min-w-0 items-center gap-2.5">
        {leading ?? <CategoryIcon icon={l.icon || (l.other ? "🏦" : "")} size="sm" />}
        <span className="min-w-0 flex-1">
          <span className="flex items-baseline gap-2">
            <span className="truncate text-sm">{l.name}</span>
            {expanded !== undefined && (
              <span className="flex shrink-0 items-center gap-0.5 text-xs text-muted">
                {l.lines?.filter((x) => x.account_id).length} debts
                <ChevronDown size={14} className={clsx("transition-transform", expanded && "rotate-180")} aria-hidden />
              </span>
            )}
            {timing && <span className="hidden shrink-0 text-xs text-muted md:inline">{timing}</span>}
            {l.hidden && <span className="shrink-0 text-xs text-muted">Hidden</span>}
            {soon > 0 && (
              <span className="shrink-0 text-xs text-upcoming" title="Recurring charges due soon (Settings → Budget sets how far ahead)">
                +{formatMoney(soon)} soon
              </span>
            )}
            {l.rollover !== 0 && (
              <span
                className={clsx("shrink-0 text-xs", l.rollover < 0 ? "text-negative" : "text-muted")}
                title="Carried over from earlier months"
                data-testid="budget-rollover"
              >
                ↻ {l.rollover > 0 ? "+" : "−"}
                {formatMoney(Math.abs(l.rollover))}
              </span>
            )}
          </span>
          {(l.budget > 0 || l.actual > 0 || soon > 0) && (
            <span className="relative mt-1 block h-1.5 rounded-full bg-surface-2" aria-hidden>
              <span
                className={clsx("absolute inset-y-0 left-0 rounded-full", over ? "bg-negative" : income ? "bg-positive" : ahead ? "bg-accent/60" : "bg-accent")}
                style={{ width: `${pct * 100}%` }}
              />
              {soonPct > 0 && (
                <span
                  className="absolute inset-y-0 rounded-r-full bg-upcoming/70"
                  style={{ left: `${pct * 100}%`, width: `${soonPct * 100}%` }}
                  data-testid="budget-upcoming"
                />
              )}
              {showPacing && l.expected > 0 && l.budget > 0 && l.expected < l.budget && (
                <span className="absolute -inset-y-0.5 w-0.5 rounded bg-text/50" style={{ left: `${(l.expected / l.budget) * 100}%` }} title="Expected by today" />
              )}
            </span>
          )}
        </span>
      </span>
      <MoneyText cents={l.budget} className="hidden text-right sm:block" />
      <Link
        to={"/transactions" as string}
        search={txns as never}
        onClick={(e) => e.stopPropagation()}
        className="justify-self-end rounded text-right hover:text-accent hover:underline"
        title="View these transactions"
        data-testid="budget-line-actual"
      >
        <MoneyText cents={l.actual} />
      </Link>
      <Remaining cents={remaining(l)} income={income} />
    </div>
  );
}

/** Remaining money: red when an expense is over budget; plain for income still to come. */
function Remaining({ cents, income }: { cents: number; income: boolean }) {
  return <MoneyText cents={cents} className={clsx("text-right", !income && cents < 0 && "text-negative", !income && cents > 0 && "text-positive")} />;
}

function Summary({ b, className }: { b: Budget; className?: string }) {
  const s = b.summary;
  const rows = [
    { label: "Income", budget: s.income_budget, actual: s.income_actual },
    { label: "Expenses", budget: s.expense_budget, actual: s.expense_actual },
    { label: "Contributions", budget: s.goals_budget, actual: s.goals_actual },
  ];
  return (
    <Card className={className}>
      <div className="text-center">
        <div className="text-[13px] text-muted">{b.view === "month" ? "Left to budget" : "Left this period"}</div>
        <MoneyText
          cents={s.left_to_budget}
          className={clsx("text-2xl font-semibold", s.left_to_budget < 0 ? "text-negative" : "text-positive")}
        />
        <div className="text-xs text-muted" data-testid="budget-left">
          income − expenses − contributions
        </div>
      </div>
      <dl className="mt-4 flex flex-col gap-3 text-[13px]">
        {rows.map((r) => (
          <div key={r.label}>
            <div className="flex justify-between">
              <dt className="font-medium">{r.label}</dt>
              <dd>
                <MoneyText cents={r.actual} /> <span className="text-muted">of</span> <MoneyText cents={r.budget} />
              </dd>
            </div>
            <div className="mt-1 h-1.5 rounded-full bg-surface-2" aria-hidden>
              <div
                className={clsx("h-full rounded-full", r.label === "Income" ? "bg-positive" : r.actual > r.budget ? "bg-negative" : "bg-accent")}
                style={{ width: `${r.budget > 0 ? Math.min(100, (r.actual / r.budget) * 100) : r.actual > 0 ? 100 : 0}%` }}
              />
            </div>
          </div>
        ))}
      </dl>
      {s.unbudgeted_spend_count > 0 && (
        <p className="mt-4 text-xs text-muted">
          {s.unbudgeted_spend_count} {s.unbudgeted_spend_count === 1 ? "category has" : "categories have"} spending but no budget.
        </p>
      )}
    </Card>
  );
}

/** The n whole months before month (YYYY-MM), as an inclusive date range. */
function pastMonths(month: string, n: number) {
  const [y, m] = month.split("-").map(Number);
  const iso = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  return { from: iso(new Date(y, m - 1 - n, 1)), to: iso(new Date(y, m - 1, 0)) };
}

/** What the chat sees from the budget page: this period line by line, and the months before. */
function budgetContext(b: Budget, history: Report | undefined): ChatContext {
  const line = (l: BudgetLine): Record<string, unknown> => ({
    name: l.name,
    budget: dollars(l.budget),
    actual: dollars(l.actual),
    remaining: dollars(l.budget - l.actual),
    ...(l.expected ? { planned_by_today: dollars(l.expected) } : {}),
    ...(l.upcoming ? { recurring_due_soon: dollars(l.upcoming) } : {}),
    ...(l.rollover ? { rolled_over: dollars(l.rollover) } : {}),
    ...(l.lines?.length ? { by_debt: l.lines.map(line) } : {}),
  });
  const months = history?.buckets.map((k) => k.start.slice(0, 7)) ?? [];
  const perMonth = (lines: Report["spending"]["lines"]) =>
    lines.filter((l) => l.total !== 0).map((l) => ({ name: l.name, total: dollars(l.total), by_month: Object.fromEntries(months.map((m, i) => [m, dollars(l.values[i] ?? 0)])) }));
  return {
    title: "your budget",
    page: `Budget › ${periodLabel(b)}`,
    suggestions: ["How am I doing this month?", "Which budgets are unrealistic compared to past months?", "Where could I cut back?", "Does my budget follow good practice?"],
    data: {
      view: b.view,
      period: { start: b.start, end: b.end, today: b.today },
      summary: Object.fromEntries(Object.entries(b.summary).map(([k, v]) => [k, k.endsWith("_count") ? v : dollars(v)])),
      groups: b.groups.map((g) => ({ name: g.name, kind: g.kind, budget: dollars(g.budget), actual: dollars(g.actual), lines: g.lines.map(line) })),
      past_months: history
        ? { months, income_by_month: history.income.values.map(dollars), spending_by_month: history.spending.values.map(dollars), spending_by_category: perMonth(history.spending.lines), income_by_category: perMonth(history.income.lines) }
        : "loading",
      debt_repayment_counts: b.settings.debt_actual === "paid" ? "total paid to each debt" : "net paydown (payments minus new charges on that debt)",
    },
  };
}
