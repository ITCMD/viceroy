import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import clsx from "clsx";
import { ChevronLeft, ChevronRight, Target as TargetIcon } from "lucide-react";
import { useState } from "react";
import { Button, Card, CategoryIcon, MoneyText, PageHeader, Segmented } from "@/components/ui";
import { BudgetEditDialog } from "./BudgetEditDialog";
import { budgetQuery, chunkLabel, periodLabel, remaining, type Budget, type BudgetGroup, type BudgetLine, type Target, type View } from "./api";

const views: { value: View; label: string }[] = [
  { value: "month", label: "Month" },
  { value: "week", label: "Week" },
  { value: "paycheck", label: "Paycheck" },
];

const VIEW_KEY = "viceroy.budget.view";

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
  const { data: b } = useQuery(budgetQuery(view, date));

  const setView = (v: View) => {
    setViewState(v);
    try {
      localStorage.setItem(VIEW_KEY, v);
    } catch {
      /* ignore */
    }
  };
  const isCurrent = b ? b.today >= b.start && b.today <= b.end : true;

  return (
    <>
      <PageHeader title="Budget" />
      <div className="mx-auto flex max-w-6xl flex-col gap-4 p-4 md:p-6">
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
                <GroupCard key={`${g.kind}-${g.id}`} g={g} showPacing={isCurrent} onEdit={(line) => setEditing({ target: { kind: g.kind === "goals" ? "goal" : "category", id: line.id }, line })} />
              ))}
            </div>
            <Summary b={b} className="order-1 lg:sticky lg:top-18 lg:order-2" />
          </div>
        )}
      </div>
      <BudgetEditDialog
        target={editing?.target ?? null}
        line={editing?.line ?? null}
        month={b?.month ?? ""}
        forwardDefault={b?.settings.forward_default ?? false}
        onClose={() => setEditing(null)}
      />
    </>
  );
}

function GroupCard({ g, showPacing, onEdit }: { g: BudgetGroup; showPacing: boolean; onEdit: (l: BudgetLine) => void }) {
  const income = g.kind === "income";
  return (
    <section className="overflow-hidden rounded-xl border border-border bg-surface" data-testid={`budget-group-${g.kind}`}>
      <header className={clsx(cols, "border-b border-border px-4 py-2.5 text-[13px]")}>
        <h3 className="text-[15px] font-semibold">{g.name}</h3>
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
          {g.lines.map((l) => (
            <li key={l.id}>
              <LineRow l={l} income={income} showPacing={showPacing} onClick={() => onEdit(l)} />
            </li>
          ))}
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

function LineRow({ l, income, showPacing, onClick }: { l: BudgetLine; income: boolean; showPacing: boolean; onClick: () => void }) {
  const pct = l.budget > 0 ? Math.min(1, l.actual / l.budget) : l.actual > 0 ? 1 : 0;
  const over = !income && l.actual > l.budget;
  const ahead = !income && showPacing && l.expected > 0 && l.actual > l.expected && !over;
  const timing = chunkLabel(l.chunk);
  return (
    <button onClick={onClick} className={clsx(cols, "w-full px-4 py-2 text-left text-[13px] hover:bg-surface-2")} data-testid="budget-line">
      <span className="flex min-w-0 items-center gap-2.5">
        <CategoryIcon icon={l.icon} size="sm" />
        <span className="min-w-0 flex-1">
          <span className="flex items-baseline gap-2">
            <span className="truncate text-sm">{l.name}</span>
            {timing && <span className="hidden shrink-0 text-xs text-muted md:inline">{timing}</span>}
          </span>
          {(l.budget > 0 || l.actual > 0) && (
            <span className="relative mt-1 block h-1.5 rounded-full bg-surface-2" aria-hidden>
              <span
                className={clsx("absolute inset-y-0 left-0 rounded-full", over ? "bg-negative" : income ? "bg-positive" : ahead ? "bg-accent/60" : "bg-accent")}
                style={{ width: `${pct * 100}%` }}
              />
              {showPacing && l.expected > 0 && l.budget > 0 && l.expected < l.budget && (
                <span className="absolute -inset-y-0.5 w-0.5 rounded bg-text/50" style={{ left: `${(l.expected / l.budget) * 100}%` }} title="Expected by today" />
              )}
            </span>
          )}
        </span>
      </span>
      <MoneyText cents={l.budget} className="hidden text-right sm:block" />
      <MoneyText cents={l.actual} className="text-right" />
      <Remaining cents={remaining(l)} income={income} />
    </button>
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
