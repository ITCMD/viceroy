import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { PartyPopper, Pencil } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Legend } from "@/components/charts/Legend";
import { SeriesChart, type Series } from "@/components/charts/SeriesChart";
import { useChartTokens } from "@/components/charts/tokens";
import { Badge, Button, Card, Dialog, EmptyState, Field, FormError, MoneyText, Segmented, StatTile, Switch } from "@/components/ui";
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
const dayText = (d: string) => new Date(d + "T00:00:00").toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" });

const when = (r: Report, p: DebtPlan) => (p.never ? "Never at this pace" : p.months === 0 ? "Now" : planMonth(r.start, p.months));
/** "2027-03" for the month n months after start (YYYY-MM). */
const planKey = (start: string, n: number) => {
  const [y, m] = start.split("-").map(Number);
  const d = new Date(y, m - 1 + n, 1);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
};

/** Same payoff order means the same simulation, so the two plans are identical. */
const sameOrder = (a: DebtPlan, b: DebtPlan) => a.debts.every((d, i) => d.order === b.debts[i]?.order);
const needsTerms = (d: Debt) => d.apr_source === "missing" || d.min_payment_source === "missing";

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
    const { minimum, snowball, avalanche } = r?.plans ?? {};
    if (!r || !minimum || !snowball || !avalanche) return { labels: [] as string[], series: [] as Series[] };
    const n = Math.min(Math.max(...[minimum, snowball, avalanche].map((p) => p.balances.length)), 360);
    const pick = (p: DebtPlan) => [r.total, ...Array.from({ length: n }, (_, i) => p.balances[i] ?? 0)];
    const line = (name: string, p: DebtPlan, color: string) => ({ name, values: pick(p), color, type: "line" as const });
    const snow = line("Snowball", snowball, t.series[1]);
    const ava = line("Avalanche", avalanche, t.series[0]);
    return {
      labels: ["Now", ...Array.from({ length: n }, (_, i) => planMonth(r.start, i + 1))],
      series: [
        { ...line("Minimums only", minimum, t.muted), dashed: true },
        // Identical plans would draw one line over the other; show one. Otherwise the chosen one goes on top.
        ...(sameOrder(snowball, avalanche) ? [line("Snowball & avalanche", avalanche, t.series[0])] : strategy === "snowball" ? [ava, snow] : [snow, ava]),
      ],
    };
  }, [r, t, strategy]);

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
          zero_percent_intro_until: d.promo_until,
          min_payment: dollars(d.min_payment),
          min_payment_source: d.min_payment_source,
          monthly_interest: dollars(d.monthly_interest),
          interest_charged_last_12_months: dollars(d.interest_paid_12m),
        })),
        payoff_plans: r.ready ? undefined : "Not available until every debt has an APR and minimum payment entered.",
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

  const missing = r.debts.filter(needsTerms);
  const missingAPR = r.debts.filter((d) => d.apr_source === "missing").length;
  const plan = r.plans[strategy];

  return (
    <>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4" data-testid="debt-stats">
        <StatTile label="Total debt">
          <MoneyText cents={r.total} whole />
        </StatTile>
        <StatTile label="Interest per month" sub={missingAPR > 0 && `Not counting ${missingAPR} debt${missingAPR === 1 ? "" : "s"} without an APR`}>
          {missingAPR === r.debts.length ? <span className="text-muted">—</span> : <span className="text-negative">{formatMoney(r.monthly_interest, { whole: true })}</span>}
        </StatTile>
        <StatTile label="Interest charged, last 12 mo">
          <MoneyText cents={r.interest_paid_12m} whole />
        </StatTile>
        <StatTile label="Debt free" sub={!plan && "Needs rates and minimums"}>
          {plan ? when(r, plan) : <span className="text-muted">—</span>}
        </StatTile>
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

      <Card title="Your debts" action={missing.length > 0 && <span className="text-xs text-muted">Enter each debt's APR and minimum from its statement</span>}>
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
                    ) : d.promo_until ? (
                      <>
                        <div>0%</div>
                        <div className="text-[11px] text-muted">
                          until {dayText(d.promo_until)}, then {aprText(d.apr_bps)}
                        </div>
                      </>
                    ) : (
                      aprText(d.apr_bps)
                    )}
                  </td>
                  <td className="py-2 text-right tabular">
                    {d.min_payment_source === "missing" ? (
                      <Badge>Not set</Badge>
                    ) : (
                      <span title={d.min_payment_source === "bill" ? "From your bank's statement email" : undefined}>{formatMoney(d.min_payment)}</span>
                    )}
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
      </Card>

      <Card title="Payoff plan">
        {r.ready ? (
          <PayoffPlan r={r} strategy={strategy} setStrategy={setStrategy} extraText={extraText} setExtraText={setExtraText} projection={projection} />
        ) : (
          <div className="flex flex-col gap-3" data-testid="debt-needs-terms">
            <p className="text-[13px] text-muted">
              A payoff plan needs each debt's APR and minimum payment, from its latest statement. Use 0% for an interest-free loan.
            </p>
            <ul className="flex flex-col gap-1.5">
              {missing.map((d) => (
                <li key={d.account_id} className="flex items-center gap-3 rounded-lg bg-surface-2 px-3 py-2 text-[13px]">
                  <AccountAvatar account={{ name: d.name, institution_name: d.institution_name, color: d.color, logo_url: d.logo_url }} size={24} />
                  <span className="min-w-0 flex-1 truncate font-medium">{d.name}</span>
                  <span className="hidden text-muted sm:block">
                    Missing {[d.apr_source === "missing" && "APR", d.min_payment_source === "missing" && "minimum"].filter(Boolean).join(" and ")}
                  </span>
                  <Button variant="secondary" size="sm" onClick={() => setEditing(d)}>
                    Set terms
                  </Button>
                </li>
              ))}
            </ul>
          </div>
        )}
      </Card>

      <DebtTermsDialog debt={editing} onClose={() => setEditing(null)} />
    </>
  );
}

function PayoffPlan({
  r,
  strategy,
  setStrategy,
  extraText,
  setExtraText,
  projection,
}: {
  r: Report;
  strategy: Strategy;
  setStrategy: (s: Strategy) => void;
  extraText: string;
  setExtraText: (v: string) => void;
  projection: { labels: string[]; series: Series[] };
}) {
  const plan = r.plans[strategy]!;
  const other = r.plans[strategy === "snowball" ? "avalanche" : "snowball"]!;
  const base = r.plans.minimum!;
  const same = sameOrder(plan, other);
  const saved = base.never ? null : base.interest - plan.interest;
  const order = [...plan.debts].sort((a, b) => a.order - b.order);
  const byId = new Map(r.debts.map((d) => [d.account_id, d]));
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end gap-3">
        <Field label="Extra each month" inputMode="decimal" value={extraText} onChange={(e) => setExtraText(e.target.value)} className="w-40" hint="On top of the minimums" />
        {!same && (
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
        )}
      </div>
      <div className={clsx("grid gap-3", same ? "sm:grid-cols-2" : "sm:grid-cols-3")} data-testid="debt-plans">
        <PlanTile title="Minimums only" plan={base} r={r} muted />
        <PlanTile title={same ? "Snowball & avalanche" : strategy === "snowball" ? "Snowball" : "Avalanche"} plan={plan} r={r} highlight />
        {!same && <PlanTile title={strategy === "snowball" ? "Avalanche" : "Snowball"} plan={other} r={r} />}
      </div>
      <Insight r={r} strategy={strategy} saved={saved} same={same} />
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
function Insight({ r, strategy, saved, same }: { r: Report; strategy: Strategy; saved: number | null; same: boolean }) {
  const snow = r.plans.snowball!;
  const ava = r.plans.avalanche!;
  const base = r.plans.minimum!;
  const first = (p: DebtPlan) => r.debts.find((d) => d.account_id === p.debts.find((x) => x.order === 1)?.id);
  const lines: string[] = [];
  if (base.never) lines.push("Paying only the minimums never clears this debt: the interest outgrows the payments.");
  if (r.extra > 0 && saved !== null && saved > 0) {
    lines.push(`${formatMoney(r.extra, { whole: true })} extra a month saves ${formatMoney(saved, { whole: true })} in interest and finishes ${span(Math.max(base.months - r.plans[strategy]!.months, 0))} sooner than minimums only.`);
  }
  if (r.debts.length > 1 && same) {
    lines.push("Snowball and avalanche come out the same here: your smallest balances also carry the highest rates, so both pay debts off in the same order.");
  } else if (r.debts.length > 1) {
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
  for (const d of r.debts) {
    const months = r.plans[strategy]!.debts.find((x) => x.id === d.account_id)?.months ?? 0;
    // Plan month n ends n months from now; past the intro's month, interest is being charged.
    if (d.promo_until && (months === 0 || planKey(r.start, months) > d.promo_until.slice(0, 7))) {
      lines.push(`${d.name}'s 0% rate ends ${dayText(d.promo_until)}, before this plan pays it off; ${aprText(d.apr_bps)} applies after that.`);
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
  const [promo, setPromo] = useState(false);
  const [until, setUntil] = useState("");
  useEffect(() => {
    if (!debt) return;
    setApr(debt.apr_source === "user" ? String(debt.apr_bps / 100) : "");
    setMin(debt.min_payment_source === "user" ? (debt.min_payment / 100).toFixed(2) : "");
    setPromo(!!debt.promo_until);
    setUntil(debt.promo_until ?? "");
  }, [debt]);
  const save = useMutation({
    mutationFn: () => api.patch(`/accounts/${debt!.account_id}`, { apr, min_payment: min, promo_until: promo ? until : "" }),
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
      description="From your latest statement. Use 0% for an interest-free loan."
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate()} loading={save.isPending} disabled={promo && !until}>
            Save
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          if (!promo || until) save.mutate();
        }}
      >
        <Switch label="0% intro APR" hint="A promo or balance transfer rate that ends on a date." checked={promo} onCheckedChange={setPromo} />
        {promo && <Field label="0% until" type="date" required value={until} onChange={(e) => setUntil(e.target.value)} />}
        <Field label={promo ? "APR after the intro (%)" : "APR (%)"} inputMode="decimal" placeholder="e.g. 24.99" value={apr} onChange={(e) => setApr(e.target.value)} />
        <Field
          label="Minimum payment ($)"
          inputMode="decimal"
          placeholder={debt?.min_payment_source === "bill" ? `${(debt.min_payment / 100).toFixed(2)} (from your bank's email)` : "e.g. 85.00"}
          value={min}
          onChange={(e) => setMin(e.target.value)}
        />
        <FormError error={save.error} />
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}
