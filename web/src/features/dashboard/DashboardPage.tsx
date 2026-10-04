import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import clsx from "clsx";
import { CalendarClock, LineChart, ShoppingBag, Sparkles, Target } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { Legend } from "@/components/charts/Legend";
import { SeriesChart, type Series } from "@/components/charts/SeriesChart";
import { useChartTokens } from "@/components/charts/tokens";
import { Button, Card, CategoryIcon, EmptyState, MoneyText, PageHeader, Glyph } from "@/components/ui";
import { CanIBuyDialog } from "@/features/canibuy/CanIBuyDialog";
import { ChatSheet } from "@/features/chat/ChatSheet";
import { CloseoutBanner } from "@/features/closeout/CloseoutBanner";
import { RiskCard } from "@/features/closeout/RiskCard";
import { accountsQuery, accountSubtitle } from "@/features/accounts/api";
import { NetWorthCard } from "@/features/accounts/NetWorthCard";
import { budgetQuery, monthLabel, type BudgetGroup } from "@/features/budget/api";
import { goalsQuery } from "@/features/goals/api";
import { dueLabel, recurringQuery } from "@/features/recurring/api";
import { spendingPaceQuery } from "@/features/reports/api";
import { formatMoney } from "@/lib/format";
import { useSession } from "@/lib/session";

function greeting() {
  const h = new Date().getHours();
  return h < 12 ? "Good morning" : h < 18 ? "Good afternoon" : "Good evening";
}

export function DashboardPage() {
  const { data: session } = useSession();
  const first = session?.user?.name.split(" ")[0];
  const [chatOpen, setChatOpen] = useState(false);
  const [buyOpen, setBuyOpen] = useState(false);
  return (
    <>
      <PageHeader
        title="Dashboard"
        actions={
          <>
            <Button size="sm" variant="secondary" onClick={() => setBuyOpen(true)}>
              <ShoppingBag size={14} className="text-accent" />
              Can I buy?
            </Button>
            <Button size="sm" variant="secondary" onClick={() => setChatOpen(true)}>
              <Sparkles size={14} className="text-accent" />
              <span className="sm:hidden">Chat</span>
              <span className="hidden sm:inline">Chat with your budget</span>
            </Button>
          </>
        }
      />
      <ChatSheet open={chatOpen} onOpenChange={setChatOpen} />
      <CanIBuyDialog open={buyOpen} onOpenChange={setBuyOpen} />
      <div className="mx-auto flex max-w-6xl flex-col gap-4 p-4 md:p-6">
        <h2 className="text-xl font-semibold tracking-tight">
          {greeting()}
          {first ? `, ${first}` : ""}
        </h2>
        <CloseoutBanner />
        <RiskCard />
        <div className="grid gap-4 lg:grid-cols-2">
          <NetWorthCard />
          <SpendingCard />
          <BudgetCard />
          <RecurringCard />
          <InvestmentsCard />
          <GoalsCard />
        </div>
      </div>
    </>
  );
}

function ViewLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <Link to={to as string} className="text-[13px] font-medium text-accent hover:underline">
      {children}
    </Link>
  );
}

// ---- spending this month vs last ----

function SpendingCard() {
  const t = useChartTokens();
  const { data } = useQuery(spendingPaceQuery);
  const series = useMemo<Series[]>(() => {
    if (!data) return [];
    const n = Math.max(data.days_in_month, data.last.length);
    const pad = (v: number[]) => Array.from({ length: n }, (_, i) => (i < v.length ? v[i] : null));
    return [
      { name: monthLabel(data.month).split(" ")[0], values: pad(data.this), color: t.series[0], type: "line" },
      { name: monthLabel(data.prev_month).split(" ")[0], values: pad(data.last), color: t.muted, type: "line", dashed: true },
    ];
  }, [data, t]);
  const labels = useMemo(() => series[0]?.values.map((_, i) => String(i + 1)) ?? [], [series]);
  const spent = data?.this.at(-1) ?? 0;
  const sameDayLast = data ? (data.last[Math.min(data.this.length, data.last.length) - 1] ?? 0) : 0;
  const diff = spent - sameDayLast;

  return (
    <Card>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="text-[13px] font-medium text-muted">Spending this month</div>
          <MoneyText cents={spent} className="text-3xl font-semibold tracking-tight" />
          {data && data.last.length > 0 && (
            <div className={clsx("mt-0.5 text-[13px] tabular", diff > 0 ? "text-negative" : diff < 0 ? "text-positive" : "text-muted")}>
              {formatMoney(Math.abs(diff))} {diff > 0 ? "more" : "less"} <span className="text-muted">than this time last month</span>
            </div>
          )}
        </div>
        <Legend items={series} />
      </div>
      <div className="mt-3">
        <SeriesChart labels={labels} series={series} label="Spending this month vs last month chart" height={220} />
      </div>
    </Card>
  );
}

// ---- budget ----

function BudgetCard() {
  const { data: b } = useQuery(budgetQuery("month", ""));
  const groups = b?.groups.filter((g) => g.kind === "fixed" || g.kind === "flexible" || g.kind === "non_monthly") ?? [];
  const s = b?.summary;
  return (
    <Card title={b ? `Budget · ${monthLabel(b.month)}` : "Budget"} action={<ViewLink to="/budget">View budget</ViewLink>}>
      {s && (
        <div className="mb-4 grid grid-cols-2 gap-3 text-[13px]">
          <div>
            <div className="text-muted">Income</div>
            <div className="font-semibold tabular">
              {formatMoney(s.income_actual, { whole: true })} <span className="font-normal text-muted">of {formatMoney(s.income_budget, { whole: true })}</span>
            </div>
          </div>
          <div>
            <div className="text-muted">Expenses</div>
            <div className="font-semibold tabular">
              {formatMoney(s.expense_actual, { whole: true })} <span className="font-normal text-muted">of {formatMoney(s.expense_budget, { whole: true })}</span>
            </div>
          </div>
        </div>
      )}
      <ul className="flex flex-col gap-3" data-testid="dashboard-budget">
        {groups.map((g) => (
          <BudgetGroupRow key={g.id} g={g} />
        ))}
      </ul>
    </Card>
  );
}

function BudgetGroupRow({ g }: { g: BudgetGroup }) {
  const left = g.budget - g.actual;
  const over = left < 0;
  const pct = g.budget > 0 ? Math.min(1, g.actual / g.budget) : g.actual > 0 ? 1 : 0;
  return (
    <li>
      <div className="flex items-baseline justify-between gap-2 text-sm">
        <span className="font-medium">{g.name}</span>
        <span className={clsx("text-[13px] tabular", over ? "text-negative" : "text-muted")}>
          {formatMoney(Math.abs(left), { whole: true })} {over ? "over" : "left"}
        </span>
      </div>
      <div className="mt-1 h-1.5 rounded-full bg-surface-2" aria-hidden>
        <div className={clsx("h-full rounded-full", over ? "bg-negative" : "bg-accent")} style={{ width: `${pct * 100}%` }} />
      </div>
      <div className="mt-0.5 text-xs text-muted tabular">
        {formatMoney(g.actual, { whole: true })} of {formatMoney(g.budget, { whole: true })}
      </div>
    </li>
  );
}

// ---- upcoming recurring ----

function RecurringCard() {
  const { data } = useQuery(recurringQuery());
  const upcoming = useMemo(() => {
    if (!data) return [];
    const until = new Date(Date.parse(data.today) + 30 * 86_400_000).toISOString().slice(0, 10);
    return data.upcoming.filter((s) => s.next_date && s.next_date <= until).slice(0, 6);
  }, [data]);
  const count = data?.upcoming.length ?? 0;
  return (
    <Card title="Upcoming recurring" action={<ViewLink to="/recurring">View all{count ? ` (${count})` : ""}</ViewLink>}>
      {upcoming.length === 0 ? (
        <EmptyState icon={CalendarClock} title="Nothing due soon">
          Recurring bills and paychecks show up here once they've repeated a few times, or when you track them on the Recurring tab.
        </EmptyState>
      ) : (
        <ul className="-my-1 divide-y divide-border" data-testid="dashboard-recurring">
          {upcoming.map((s) => (
            <li key={s.key} className="flex items-center gap-3 py-2">
              <CategoryIcon icon={s.category_icon} size="sm" />
              <span className="min-w-0 flex-1 truncate text-sm">{s.name}</span>
              <span className={clsx("text-xs", s.next_date < data!.today ? "text-negative" : "text-muted")}>{dueLabel(s.next_date, data!.today)}</span>
              <MoneyText cents={s.amount} colored className="w-20 text-right text-sm" />
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

// ---- investments ----

function InvestmentsCard() {
  const { data } = useQuery(accountsQuery);
  const list = data?.accounts.filter((a) => a.group === "investments" && !a.hidden && a.status !== "ignored") ?? [];
  const total = list.reduce((s, a) => s + a.balance_cents, 0);
  return (
    <Card title="Investments" action={<ViewLink to="/accounts">Accounts</ViewLink>}>
      {list.length === 0 ? (
        <EmptyState icon={LineChart} title="No investment accounts">
          Brokerage and retirement accounts from SimpleFIN appear here.
        </EmptyState>
      ) : (
        <>
          <MoneyText cents={total} className="text-2xl font-semibold tracking-tight" />
          <ul className="mt-2 divide-y divide-border">
            {list.map((a) => (
              <li key={a.id} className="flex items-center justify-between gap-3 py-2 text-sm">
                <span className="min-w-0">
                  <span className="block truncate">{a.name}</span>
                  <span className="block truncate text-xs text-muted">{accountSubtitle(a)}</span>
                </span>
                <MoneyText cents={a.balance_cents} />
              </li>
            ))}
          </ul>
        </>
      )}
    </Card>
  );
}

// ---- goals ----

function GoalsCard() {
  const { data } = useQuery(goalsQuery);
  const goals = data?.goals.filter((g) => !g.archived).slice(0, 4) ?? [];
  return (
    <Card title="Goals" action={<ViewLink to="/goals">View goals</ViewLink>}>
      {goals.length === 0 ? (
        <EmptyState icon={Target} title="No goals yet">
          <ViewLink to="/goals">Create a goal</ViewLink> to save toward something.
        </EmptyState>
      ) : (
        <ul className="flex flex-col gap-3" data-testid="dashboard-goals">
          {goals.map((g) => {
            const pct = g.target_cents > 0 ? Math.min(1, g.balance_cents / g.target_cents) : 0;
            return (
              <li key={g.id}>
                <div className="flex items-baseline justify-between gap-2 text-sm">
                  <span className="truncate font-medium">
                    {g.icon && <><Glyph icon={g.icon} />{" "}</>}
                    {g.name}
                  </span>
                  <span className="text-[13px] text-muted tabular">
                    {formatMoney(g.balance_cents, { whole: true })}
                    {g.target_cents > 0 && ` of ${formatMoney(g.target_cents, { whole: true })}`}
                  </span>
                </div>
                <div className="mt-1 h-1.5 rounded-full bg-surface-2" aria-hidden>
                  <div className="h-full rounded-full bg-accent" style={{ width: `${pct * 100}%` }} />
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </Card>
  );
}
