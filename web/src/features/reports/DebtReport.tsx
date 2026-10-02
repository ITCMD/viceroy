import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { PartyPopper, Pencil } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Legend } from "@/components/charts/Legend";
import { SeriesChart, type Series } from "@/components/charts/SeriesChart";
import { useChartTokens } from "@/components/charts/tokens";
import { Badge, Button, Card, Dialog, EmptyState, Field, FormError, MoneyText, Segmented, StatTile } from "@/components/ui";
import { AccountAvatar } from "@/features/accounts/AccountAvatar";
import type { ChatContext } from "@/features/chat/api";
import { api } from "@/lib/api";
import { formatMoney } from "@/lib/format";
import { debtQuery, planMonth, type Debt, type DebtPlan, type DebtReport as Report } from "./api";
import { dollars } from "./shared";

type Strategy = "snowball" | "avalanche";

const EXTRA_KEY = "viceroy.debt.extra";

function loadExtra() {
  try {
    const v = Number(localStorage.getItem(EXTRA_KEY));
    return Number.isFinite(v) && v >= 0 ? v : 10000;
  } catch {
    return 10000;
  }
}

const aprText = (bps: number) => `${(bps / 100).toFixed(2).replace(/\.?0+$/, "")}%`;

const when = (r: Report, p: DebtPlan) => (p.never ? "Never at this pace" : p.months === 0 ? "Now" : planMonth(r.start, p.months));
const span = (months: number) => {
  const y = Math.floor(months / 12);
  const m = months % 12;
  return [y && `${y} yr`, m && `${m} mo`].filter(Boolean).join(" ") || "0 mo";
};

export function DebtReport({ onContext }: { onContext: (c: ChatContext) => void }) {
  const t = useChartTokens();
  const [extraText, setExtraText] = useState(() => String(loadExtra() / 100));
  const [extra, setExtra] = useState(loadExtra);
  const [strategy, setStrategy] = useState<Strategy>("avalanche");
  const [editing, setEditing] = useState<Debt | null>(null);
  useEffect(() => {
    const id = setTimeout(() => {
      const v = Math.round(Number(extraText.replace(/[$,]/g, "")) * 100);
      if (Number.isFinite(v) && v >= 0) {
        setExtra(v);
        try {
          localStorage.setItem(EXTRA_KEY, String(v));
        } catch {
          /* ignore */
        }
      }
    }, 300);
    return () => clearTimeout(id);
  }, [extraText]);
  const { data: r } = useQuery(debtQuery(extra));

  const colors = useMemo(() => new Map((r?.debts ?? []).map((d, i) => [d.account_id, d.color || t.series[i % t.series.length]])), [r, t]);
  const history = useMemo(() => {
    if (!r) return { labels: [] as string[], series: [] as Series[] };
    const ids = new Set<number>();
    r.history.forEach((p) => Object.keys(p.accounts).forEach((k) => ids.add(Number(k))));
    const names = new Map(r.debts.map((d) => [d.account_id, d.name]));
    return {
      labels: r.history.map((p, i) =>
        i === r.history.length - 1 ? "Now" : new Date(p.date + "T00:00:00").toLocaleDateString("en-US", { month: "short", year: "2-digit" }),
      ),
      series: [...ids].map((id, i) => ({
        name: names.get(id) ?? "Paid off",
        values: r.history.map((p) => p.accounts[id] ?? 0),
        color: colors.get(id) ?? t.series[(i + 3) % t.series.length],
        stack: "a",
      })),
    };
  }, [r, colors, t]);
  const projection = useMemo(() => {
    if (!r) return { labels: [] as string[], series: [] as Series[] };
    const plans = [r.plans.minimum, r.plans.snowball, r.plans.avalanche];
    const n = Math.min(Math.max(...plans.map((p) => p.balances.length)), 360);
    const pick = (p: DebtPlan) => [r.total, ...Array.from({ length: n }, (_, i) => p.balances[i] ?? 0)];
    return {
      labels: ["Now", ...Array.from({ length: n }, (_, i) => planMonth(r.start, i + 1))],
      series: [
        { name: "Minimums only", values: pick(r.plans.minimum), color: t.muted, type: "line" as const, dashed: true },
        { name: "Snowball", values: pick(r.plans.snowball), color: t.series[1], type: "line" as const },
        { name: "Avalanche", values: pick(r.plans.avalanche), color: t.series[0], type: "line" as const },
      ],
    };
  }, [r, t]);

  useEffect(() => {
    if (!r) return;
    onContext({
      title: "Debt Free Future",
      page: "Reports › Debt Free Future",
      suggestions: ["Which debt should I pay off first?", "How much is interest costing me?", "What if I paid $300 more a month?", "Snowball or avalanche for me?"],
      data: {
        total_owed: dollars(r.total),
        monthly_interest: dollars(r.monthly_interest),
        interest_charged_last_12_months: dollars(r.interest_paid_12m),
        extra_per_month: dollars(r.extra),
        chosen_strategy: strategy,
        debts: r.debts.map((d) => ({
          name: d.name,
          type: d.type,
          owed: dollars(d.balance),
          apr_percent: d.apr_bps / 100,
          apr_source: d.apr_source,
          min_payment: dollars(d.min_payment),
          min_payment_source: d.min_payment_source,
          monthly_interest: dollars(d.monthly_interest),
          interest_charged_last_12_months: dollars(d.interest_paid_12m),
        })),
        plans: Object.fromEntries(
          Object.entries(r.plans).map(([k, p]) => [
            k,
            {
              monthly_payment: dollars(p.payment),
              debt_free: when(r, p),
              total_interest: dollars(p.interest),
              payoff_order: [...p.debts].sort((a, b) => a.order - b.order).map((d) => ({ name: r.debts.find((x) => x.account_id === d.id)?.name, paid_off: d.months ? planMonth(r.start, d.months) : "never" })),
            },
          ]),
        ),
        balance_history: r.history.map((p) => ({ date: p.date, owed: dollars(p.total) })),
      },
    });
  }, [onContext, r, strategy]);

  if (!r) return <Card><div className="h-64" /></Card>;
  if (r.debts.length === 0) {
    return (
      <Card>
        <EmptyState icon={PartyPopper} title="No debt to pay off">
          Credit cards and loans you owe on show up here with a plan to pay them off.
        </EmptyState>
      </Card>
    );
  }

  const plan = r.plans[strategy];
  const other = r.plans[strategy === "snowball" ? "avalanche" : "snowball"];
  const base = r.plans.minimum;
  const saved = base.never ? null : base.interest - plan.interest;
  const order = [...plan.debts].sort((a, b) => a.order - b.order);
  const byId = new Map(r.debts.map((d) => [d.account_id, d]));
  const guessed = r.debts.some((d) => d.apr_source !== "user" || d.min_payment_source === "estimate");

  return (
    <>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4" data-testid="debt-stats">
        <StatTile label="Total debt">
          <MoneyText cents={r.total} whole />
        </StatTile>
        <StatTile label="Interest per month">
          <span className="text-negative">{formatMoney(r.monthly_interest, { whole: true })}</span>
        </StatTile>
        <StatTile label="Interest charged, last 12 mo">
          <MoneyText cents={r.interest_paid_12m} whole />
        </StatTile>
        <StatTile label="Debt free">{when(r, plan)}</StatTile>
      </div>

      <Card title="Debt over time">
        {history.series.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted">Balance history builds up as accounts sync.</p>
        ) : (
          <>
            <Legend items={history.series} />
            <SeriesChart labels={history.labels} series={history.series} label="Debt over time chart" height={240} hideZero barMaxWidth={36} />
          </>
        )}
      </Card>

      <Card title="Your debts" action={guessed && <span className="text-xs text-muted">Enter rates and minimums for a better plan</span>}>
        <div className="-mx-4 overflow-x-auto px-4">
          <table className="w-full min-w-[560px] text-[13px]" data-testid="debt-table">
            <thead>
              <tr className="text-left text-xs text-muted">
                <th className="pb-2 font-medium">Debt</th>
                <th className="pb-2 text-right font-medium">Owed</th>
                <th className="pb-2 text-right font-medium">APR</th>
                <th className="pb-2 text-right font-medium">Minimum</th>
                <th className="pb-2 text-right font-medium">Interest / mo</th>
                <th className="pb-2" />
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {r.debts.map((d) => (
                <tr key={d.account_id} data-testid="debt-row">
                  <td className="py-2 pr-2">
                    <div className="flex min-w-0 items-center gap-2">
                      <AccountAvatar account={{ name: d.name, institution_name: d.institution_name, color: d.color, logo_url: d.logo_url }} size={24} />
                      <span className="truncate font-medium">{d.name}</span>
                    </div>
                  </td>
                  <td className="py-2 text-right font-medium">
                    <MoneyText cents={d.balance} />
                  </td>
                  <td className="py-2 text-right tabular">
                    {d.apr_source === "missing" ? (
                      <Badge>Not set</Badge>
                    ) : (
                      <span className={clsx(d.apr_source === "assumed" && "text-muted")} title={d.apr_source === "assumed" ? "Assumed: a typical card rate" : undefined}>
                        {aprText(d.apr_bps)}
                        {d.apr_source === "assumed" && "*"}
                      </span>
                    )}
                  </td>
                  <td className="py-2 text-right tabular">
                    <span className={clsx(d.min_payment_source === "estimate" && "text-muted")} title={d.min_payment_source === "estimate" ? "Estimated" : d.min_payment_source === "bill" ? "From your bank's email" : undefined}>
                      {formatMoney(d.min_payment)}
                      {d.min_payment_source === "estimate" && "*"}
                    </span>
                  </td>
                  <td className="py-2 text-right text-negative tabular">{formatMoney(d.monthly_interest)}</td>
                  <td className="py-2 pl-2 text-right">
                    <Button variant="ghost" size="sm" aria-label={`Edit ${d.name} rate and minimum`} onClick={() => setEditing(d)}>
                      <Pencil size={14} />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {guessed && <p className="mt-2 text-xs text-muted">* Estimated. Cards without a rate use 22%; loans without one count as 0% until you add it.</p>}
      </Card>

      <Card title="Payoff plan">
        <div className="flex flex-col gap-4">
          <div className="flex flex-wrap items-end gap-3">
            <Field
              label="Extra each month"
              inputMode="decimal"
              value={extraText}
              onChange={(e) => setExtraText(e.target.value)}
              className="w-40"
              hint="On top of the minimums"
            />
            <Segmented
              label="Strategy"
              value={strategy}
              onChange={setStrategy}
              className="mb-5"
              items={[
                { value: "avalanche", label: "Avalanche · highest rate first" },
                { value: "snowball", label: "Snowball · smallest first" },
              ]}
            />
          </div>
          <div className="grid gap-3 sm:grid-cols-3" data-testid="debt-plans">
            <PlanTile title="Minimums only" plan={base} r={r} muted />
            <PlanTile title={strategy === "snowball" ? "Snowball" : "Avalanche"} plan={plan} r={r} highlight />
            <PlanTile title={strategy === "snowball" ? "Avalanche" : "Snowball"} plan={other} r={r} />
          </div>
          <Insight r={r} strategy={strategy} saved={saved} />
          <Legend items={projection.series} />
          <SeriesChart labels={projection.labels} series={projection.series} label="Projected debt chart" height={240} />
          <div>
            <h3 className="mb-2 text-[13px] font-semibold">Order to pay off</h3>
            <ol className="flex flex-col gap-1.5" data-testid="debt-order">
              {order.map((o) => {
                const d = byId.get(o.id);
                if (!d) return null;
                return (
                  <li key={o.id} className="flex items-center gap-3 rounded-lg bg-surface-2 px-3 py-2 text-[13px]">
                    <span className="grid size-6 shrink-0 place-items-center rounded-full bg-surface text-xs font-semibold">{o.order}</span>
                    <span className="min-w-0 flex-1 truncate font-medium">{d.name}</span>
                    <span className="text-muted">{o.months ? `Paid off ${planMonth(r.start, o.months)}` : "Not paid off at this pace"}</span>
                    <span className="hidden w-32 text-right text-muted sm:block">{formatMoney(o.interest, { whole: true })} interest</span>
                  </li>
                );
              })}
            </ol>
          </div>
        </div>
      </Card>

      <DebtTermsDialog debt={editing} onClose={() => setEditing(null)} />
    </>
  );
}

function PlanTile({ title, plan, r, highlight, muted }: { title: string; plan: DebtPlan; r: Report; highlight?: boolean; muted?: boolean }) {
  return (
    <div className={clsx("rounded-lg border p-3", highlight ? "border-accent/50 bg-accent-soft" : "border-border")}>
      <div className={clsx("text-xs font-medium", muted ? "text-muted" : "text-text")}>{title}</div>
      <div className="mt-1 text-lg font-semibold">{when(r, plan)}</div>
      <div className="mt-0.5 text-xs text-muted">
        {plan.never ? "Payments don't cover the interest" : `${span(plan.months)} · ${formatMoney(plan.interest, { whole: true })} interest`}
      </div>
      <div className="mt-0.5 text-xs text-muted">{formatMoney(plan.payment, { whole: true })} / month</div>
    </div>
  );
}

/** Plain-language read on the plan: what extra buys, and how the two strategies differ here. */
function Insight({ r, strategy, saved }: { r: Report; strategy: Strategy; saved: number | null }) {
  const snow = r.plans.snowball;
  const ava = r.plans.avalanche;
  const base = r.plans.minimum;
  const first = (p: DebtPlan) => r.debts.find((d) => d.account_id === p.debts.find((x) => x.order === 1)?.id);
  const lines: string[] = [];
  if (base.never) lines.push("Paying only the minimums never clears this debt: the interest outgrows the payments.");
  if (r.extra > 0 && saved !== null && saved > 0) {
    lines.push(`${formatMoney(r.extra, { whole: true })} extra a month saves ${formatMoney(saved, { whole: true })} in interest and finishes ${span(Math.max(base.months - r.plans[strategy].months, 0))} sooner than minimums only.`);
  }
  if (r.debts.length > 1) {
    const diff = snow.interest - ava.interest;
    const sf = first(snow);
    const af = first(ava);
    if (sf && af && sf.account_id !== af.account_id) {
      lines.push(
        `Snowball clears ${sf.name} first for a quick win; avalanche goes after ${af.name}'s ${aprText(af.apr_bps)} rate${diff > 0 ? ` and saves ${formatMoney(diff, { whole: true })} more in interest` : ""}.`,
      );
    } else if (sf) {
      lines.push(`Both strategies start with ${sf.name}: it's the smallest balance and the highest rate.`);
    }
  }
  const top = [...r.debts].sort((a, b) => b.monthly_interest - a.monthly_interest)[0];
  if (top && top.monthly_interest > 0) lines.push(`${top.name} costs the most: about ${formatMoney(top.monthly_interest, { whole: true })} in interest every month.`);
  if (lines.length === 0) return null;
  return (
    <ul className="flex list-disc flex-col gap-1 pl-5 text-[13px]" data-testid="debt-insight">
      {lines.map((l) => (
        <li key={l}>{l}</li>
      ))}
    </ul>
  );
}

function DebtTermsDialog({ debt, onClose }: { debt: Debt | null; onClose: () => void }) {
  const qc = useQueryClient();
  const [apr, setApr] = useState("");
  const [min, setMin] = useState("");
  useEffect(() => {
    if (!debt) return;
    setApr(debt.apr_source === "user" ? String(debt.apr_bps / 100) : "");
    setMin(debt.min_payment_source === "user" ? (debt.min_payment / 100).toFixed(2) : "");
  }, [debt]);
  const save = useMutation({
    mutationFn: () => api.patch(`/accounts/${debt!.account_id}`, { apr, min_payment: min }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["reports", "debt"] });
      qc.invalidateQueries({ queryKey: ["accounts"] });
      onClose();
    },
  });
  return (
    <Dialog
      open={!!debt}
      onOpenChange={(o) => !o && onClose()}
      title={debt ? `${debt.name} terms` : ""}
      description="From your statement. Leave a field empty to let Viceroy estimate it."
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate()} loading={save.isPending}>
            Save
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Field label="APR (%)" inputMode="decimal" placeholder={debt?.apr_source === "assumed" ? "22 (assumed)" : "e.g. 24.99"} value={apr} onChange={(e) => setApr(e.target.value)} />
        <Field
          label="Minimum payment ($)"
          inputMode="decimal"
          placeholder={debt ? `${(debt.min_payment / 100).toFixed(2)} (${debt.min_payment_source === "bill" ? "from your bank's email" : "estimated"})` : ""}
          value={min}
          onChange={(e) => setMin(e.target.value)}
        />
        <FormError error={save.error} />
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}
