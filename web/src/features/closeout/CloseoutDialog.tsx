import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import clsx from "clsx";
import { Plus, RefreshCw, Sparkles, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Button, Dialog, Field, FormError, Glyph, Markdown, MoneyText, Segmented, Select, StatTile, withIcon } from "@/components/ui";
import { monthLabel, useBudgetMutation } from "@/features/budget/api";
import { ApiError, api } from "@/lib/api";
import { formatMoney, timeAgo, toCents } from "@/lib/format";
import { RiskGauge } from "./RiskCard";
import { closeoutQuery, riskLabel, riskTone, type Closeout, type Destination, type ReviewLine } from "./api";

type Step = "review" | "leftover" | "next";
type Row = { key: number; dest: string; amount: string };

const monthName = (m: string) => monthLabel(m).split(" ")[0];
const destKey = (d: Pick<Destination, "id" | "kind">) => (d.kind === "goal" ? `goal:${d.id}` : `cat:${d.id}`);

/** Close out a month: review where it went over and under, put leftover money toward goals or
 * next month's budget, then see (and ask the AI about) next month's risk of overspending. */
export function CloseoutDialog({ month, open, onOpenChange }: { month: string; open: boolean; onOpenChange: (v: boolean) => void }) {
  const { data: c, error } = useQuery({
    ...closeoutQuery(month),
    enabled: open && !!month,
  });
  const [step, setStep] = useState<Step>("review");
  const loaded = !!c;
  useEffect(() => {
    if (open && c) setStep(c.closed ? "next" : "review");
    // Only when opening (or once the data first arrives).
  }, [open, loaded]);

  const steps: { value: Step; label: string }[] = [
    { value: "review", label: "1. Review" },
    { value: "leftover", label: "2. Leftover" },
    {
      value: "next",
      label: `3. ${c ? monthName(c.next_month) : "Next month"}`,
    },
  ];
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Close out ${monthLabel(month)}`}
      description={
        c && !c.closed ? `Open until ${new Date(c.closes + "T00:00:00").toLocaleDateString("en-US", { month: "short", day: "numeric" })}` : undefined
      }
      className="!max-w-2xl"
    >
      {error ? (
        <FormError error={error} />
      ) : !c ? (
        <div className="h-40" />
      ) : (
        <div className="flex flex-col gap-4" data-testid="closeout-dialog">
          <Segmented label="Close-out step" value={step} onChange={(s) => (s !== "next" || c.closed) && setStep(s)} items={steps} className="self-start" />
          {step === "review" && <ReviewStep c={c} onNext={() => setStep("leftover")} />}
          {step === "leftover" &&
            (c.closed ? (
              <Allocated c={c} onNext={() => setStep("next")} />
            ) : (
              <AllocateStep c={c} onBack={() => setStep("review")} onClosed={() => setStep("next")} />
            ))}
          {step === "next" && (c.closed ? <NextStep c={c} onDone={() => onOpenChange(false)} /> : <div className="h-40" />)}
        </div>
      )}
    </Dialog>
  );
}

// ---- step 1: review ----

function ReviewStep({ c, onNext }: { c: Closeout; onNext: () => void }) {
  const r = c.review;
  const under = r.net >= 0;
  return (
    <>
      <div className="grid grid-cols-3 gap-2">
        <StatTile label="Budgeted">{formatMoney(r.budget, { whole: true })}</StatTile>
        <StatTile label="Spent">{formatMoney(r.actual, { whole: true })}</StatTile>
        <StatTile label={under ? "Left over" : "Overspent"}>
          <span className={under ? "text-positive" : "text-negative"} data-testid="closeout-net">
            {formatMoney(Math.abs(r.net), { whole: true })}
          </span>
        </StatTile>
      </div>
      <p className="-mt-1 text-xs text-muted">Fixed and flexible categories. Non-monthly categories keep their leftovers automatically.</p>
      {r.income_budget > 0 && r.income_actual < r.income_budget && (
        <p className="rounded-lg bg-surface-2 px-3 py-2 text-[13px]">
          Income came in {formatMoney(r.income_budget - r.income_actual, { whole: true })} under plan, so some of what's left over may not be there.
        </p>
      )}
      <ReviewList title="Overspent" lines={r.over} empty="Nothing went over budget. Nice." />
      <ReviewList title="Underspent" lines={r.under} empty="No category came in under budget." />
      {r.on_target > 0 && (
        <p className="text-[13px] text-muted">
          {r.on_target} {r.on_target === 1 ? "category" : "categories"} landed right on budget.
        </p>
      )}
      <div className="flex justify-end">
        <Button onClick={onNext}>Next</Button>
      </div>
    </>
  );
}

function ReviewList({ title, lines, empty }: { title: string; lines: ReviewLine[]; empty: string }) {
  const over = title === "Overspent";
  const total = lines.reduce((n, l) => n + Math.abs(l.diff), 0);
  return (
    <section className="overflow-hidden rounded-xl border border-border" data-testid={`closeout-${over ? "over" : "under"}`}>
      <header className="flex items-center justify-between bg-surface-2 px-3 py-2 text-[13px] font-semibold">
        <span>{title}</span>
        {lines.length > 0 && <span className={clsx("tabular", over ? "text-negative" : "text-positive")}>{formatMoney(total, { whole: true })}</span>}
      </header>
      {lines.length === 0 ? (
        <p className="px-3 py-2.5 text-[13px] text-muted">{empty}</p>
      ) : (
        <ul className="divide-y divide-border">
          {lines.map((l) => (
            <li key={l.id} className="flex items-center gap-2 px-3 py-2 text-sm">
              <Glyph icon={l.icon} className="shrink-0" />
              <span className="min-w-0 flex-1 truncate">
                {l.name}
                {l.hidden && <span className="ml-1.5 text-xs text-muted">Not budgeted</span>}
              </span>
              <span className="hidden text-[13px] text-muted tabular sm:inline">
                {formatMoney(l.actual, { whole: true })} of {formatMoney(l.budget, { whole: true })}
              </span>
              <span className={clsx("w-28 text-right font-medium tabular", over ? "text-negative" : "text-positive")}>
                {formatMoney(Math.abs(l.diff), { whole: true })} {over ? "over" : "under"}
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

// ---- step 2: leftover money ----

let rowKey = 0;

function AllocateStep({ c, onBack, onClosed }: { c: Closeout; onBack: () => void; onClosed: () => void }) {
  const surplus = c.review.surplus;
  const [rows, setRows] = useState<Row[]>([]);
  const next = monthName(c.next_month);
  const byKey = useMemo(() => new Map(c.destinations.map((d) => [destKey(d), d])), [c.destinations]);
  const used = new Set(rows.map((r) => r.dest));
  const cents = rows.map((r) => toCents(r.amount));
  const total = cents.reduce<number>((n, v) => n + (v ?? 0), 0);
  const left = surplus - total;
  const invalid = rows.some((r, i) => !r.dest || !cents[i]) || left < 0;

  const options = (current: string) => [
    { value: "", label: "Choose where it goes…" },
    ...c.destinations
      .filter((d) => destKey(d) === current || !used.has(destKey(d)))
      .map((d) => ({
        value: destKey(d),
        label: d.kind === "goal" ? `${withIcon(d.icon, d.name)} · goal` : `${withIcon(d.icon, d.name)} · ${next} budget`,
      })),
  ];
  const add = (dest = "", amount = "") => setRows((rs) => [...rs, { key: ++rowKey, dest, amount }]);
  const update = (key: number, patch: Partial<Row>) => setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...patch } : r)));
  // Categories likely to run over next month are the obvious place for leftover money.
  const suggestions = c.outlook.at_risk.filter((n) => byKey.has(`cat:${n.id}`) && !used.has(`cat:${n.id}`)).slice(0, 3);

  const close = useBudgetMutation(
    () =>
      api.post(`/closeout/${c.month}`, {
        allocations: rows.map((r) => {
          const [kind, id] = r.dest.split(":");
          return {
            [kind === "goal" ? "goal_id" : "category_id"]: Number(id),
            amount: r.amount,
          };
        }),
      }),
    onClosed,
  );

  return (
    <>
      {surplus > 0 ? (
        <div className="flex flex-col gap-3">
          <p className="text-sm">
            You have <MoneyText cents={surplus} className="font-semibold text-positive" /> left over from {monthName(c.month)}. Put it toward a goal or give a
            category extra room in {next}; anything you don't assign stays as cash.
          </p>
          {suggestions.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-xs text-muted">Likely to run over in {next}:</span>
              {suggestions.map((n) => (
                <button
                  key={n.id}
                  type="button"
                  onClick={() => add(`cat:${n.id}`, String(Math.min(n.gap, Math.max(left, 0)) / 100))}
                  disabled={left <= 0}
                  className="inline-flex items-center gap-1 rounded-full border border-border px-2.5 py-1 text-xs hover:bg-surface-2 disabled:opacity-50"
                >
                  <Plus size={12} /> {withIcon(n.icon, n.name)} {formatMoney(n.gap, { whole: true })}
                </button>
              ))}
            </div>
          )}
          {rows.map((r, i) => (
            <div key={r.key} className="flex items-end gap-2" data-testid="closeout-allocation">
              <Select
                label="Destination"
                hideLabel={i > 0}
                className="min-w-0 flex-1"
                value={r.dest}
                onChange={(e) => update(r.key, { dest: e.target.value })}
                options={options(r.dest)}
              />
              <Field
                label="Amount"
                hideLabel={i > 0}
                className="w-28"
                inputMode="decimal"
                placeholder="0.00"
                value={r.amount}
                onChange={(e) => update(r.key, { amount: e.target.value })}
              />
              <Button variant="ghost" size="sm" className="mb-0.5" aria-label="Remove" onClick={() => setRows((rs) => rs.filter((x) => x.key !== r.key))}>
                <X size={14} />
              </Button>
            </div>
          ))}
          <div className="flex items-center justify-between gap-2">
            <Button
              variant="secondary"
              size="sm"
              onClick={() => add("", left > 0 ? String(left / 100) : "")}
              disabled={left <= 0 || used.size >= c.destinations.length}
            >
              <Plus size={14} /> Add destination
            </Button>
            <span className={clsx("text-[13px] tabular", left < 0 ? "text-negative" : "text-muted")} data-testid="closeout-left">
              {left < 0 ? `${formatMoney(-left)} more than you have` : `${formatMoney(left)} stays as cash`}
            </span>
          </div>
          {rows.some((r) => r.dest.startsWith("cat:")) && (
            <p className="text-xs text-muted">Categories get a one-time raise to their {next} budget; later months keep their usual amount.</p>
          )}
        </div>
      ) : (
        <p className="text-sm">
          {monthName(c.month)} ended <MoneyText cents={-c.review.net} className="font-semibold text-negative" /> over budget overall, so there's nothing left to
          put aside. Close it out to see how {next} is shaping up.
        </p>
      )}
      <FormError error={close.error} />
      <div className="flex justify-end gap-2">
        <Button variant="ghost" onClick={onBack}>
          Back
        </Button>
        <Button onClick={() => close.mutate(undefined)} loading={close.isPending} disabled={invalid} data-testid="closeout-submit">
          Close out {monthName(c.month)}
        </Button>
      </div>
    </>
  );
}

/** Where a closed month's leftover money went. */
function Allocated({ c, onNext }: { c: Closeout; onNext: () => void }) {
  const allocs = c.closed?.allocations ?? [];
  const total = allocs.reduce((n, a) => n + a.amount, 0);
  const cash = c.review.surplus - total;
  return (
    <>
      {c.review.surplus === 0 ? (
        <p className="text-sm text-muted">There was nothing left over to put aside.</p>
      ) : (
        <ul className="divide-y divide-border rounded-xl border border-border">
          {allocs.map((a, i) => (
            <li key={i} className="flex items-center gap-2 px-3 py-2 text-sm">
              <Glyph icon={a.icon} className="shrink-0" />
              <span className="flex-1">
                {a.name} <span className="text-muted">· {a.goal_id ? "goal" : `${monthName(a.month ?? c.next_month)} budget`}</span>
              </span>
              <MoneyText cents={a.amount} className="font-medium" />
            </li>
          ))}
          {cash > 0 && (
            <li className="flex items-center gap-2 px-3 py-2 text-sm text-muted">
              <span className="flex-1">Kept as cash</span>
              <MoneyText cents={cash} />
            </li>
          )}
        </ul>
      )}
      <div className="flex justify-end">
        <Button onClick={onNext}>Next</Button>
      </div>
    </>
  );
}

// ---- step 3: next month ----

function NextStep({ c, onDone }: { c: Closeout; onDone: () => void }) {
  const qc = useQueryClient();
  const o = c.outlook;
  const closed = c.closed!;
  const analyze = useMutation({
    mutationFn: () => api.post<{ analysis: string }>(`/closeout/${c.month}/analysis`),
    onSettled: () => qc.invalidateQueries({ queryKey: ["budget", "closeout", c.month] }),
  });
  // Ask the AI once, right after closing.
  useEffect(() => {
    if (!closed.analysis && !analyze.isPending && !analyze.error && !analyze.isSuccess) analyze.mutate();
  }, [closed.analysis]);
  const reopen = useBudgetMutation(() => api.del(`/closeout/${c.month}`), onDone);
  const notSetUp = analyze.error instanceof ApiError && analyze.error.status === 503;
  const next = monthName(c.next_month);

  return (
    <>
      <div className="flex items-center gap-4 rounded-xl border border-border p-3">
        <RiskGauge score={o.score} level={o.level} size={96} />
        <div className="min-w-0 flex-1">
          <div className="text-[13px] font-medium text-muted">Risk of overspending in {next}</div>
          <div className={clsx("text-lg font-semibold", riskTone[o.level].text)} data-testid="closeout-outlook-level">
            {riskLabel[o.level]}
          </div>
          <p className="text-[13px] text-muted">
            {o.at_risk.length > 0
              ? `${o.at_risk.length} ${o.at_risk.length === 1 ? "category usually costs" : "categories usually cost"} about ${formatMoney(o.over, { whole: true })} more than budgeted.`
              : `${next}'s budget covers what you usually spend.`}
          </p>
        </div>
      </div>
      {o.at_risk.length > 0 && (
        <ul className="divide-y divide-border rounded-xl border border-border">
          {o.at_risk.map((n) => (
            <li key={n.id} className="flex items-center gap-2 px-3 py-2 text-sm">
              <Glyph icon={n.icon} className="shrink-0" />
              <span className="min-w-0 flex-1 truncate">{n.name}</span>
              <span className="hidden text-[13px] text-muted tabular sm:inline">
                {n.recurring >= n.average && n.recurring > 0
                  ? `bills ${formatMoney(n.recurring, { whole: true })}`
                  : `${o.months}-mo avg ${formatMoney(n.average, { whole: true })}`}{" "}
                vs {formatMoney(n.budget, { whole: true })}
              </span>
              <span className="w-16 text-right font-medium text-negative tabular">+{formatMoney(n.gap, { whole: true })}</span>
            </li>
          ))}
        </ul>
      )}
      <section className="rounded-xl border border-border bg-surface-2/40 p-3" data-testid="closeout-analysis">
        <header className="mb-2 flex items-center justify-between gap-2">
          <span className="flex items-center gap-1.5 text-[13px] font-semibold">
            <Sparkles size={14} className="text-accent" /> AI analysis
          </span>
          {closed.analysis && (
            <Button variant="ghost" size="sm" onClick={() => analyze.mutate()} loading={analyze.isPending}>
              <RefreshCw size={13} /> Refresh
            </Button>
          )}
        </header>
        {notSetUp ? (
          <p className="text-[13px] text-muted">
            Add an OpenRouter key in{" "}
            <Link to={"/settings" as string} hash="ai" className="font-medium text-accent hover:underline">
              Settings → AI
            </Link>{" "}
            to get an analysis of {next}.
          </p>
        ) : analyze.isPending && !closed.analysis ? (
          <p className="animate-pulse text-[13px] text-muted">
            Looking at {monthName(c.month)} and your {next} budget…
          </p>
        ) : analyze.error && !closed.analysis ? (
          <div className="flex items-center gap-2">
            <FormError error={analyze.error} />
            <Button variant="secondary" size="sm" onClick={() => analyze.mutate()}>
              Try again
            </Button>
          </div>
        ) : closed.analysis ? (
          <>
            <div className="text-sm [overflow-wrap:anywhere]">
              <Markdown text={closed.analysis} />
            </div>
            {closed.analysis_at && <p className="mt-2 text-xs text-muted">Analyzed {timeAgo(closed.analysis_at)}</p>}
          </>
        ) : null}
      </section>
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs text-muted">
          Closed {timeAgo(closed.closed_at)}
          {closed.closed_by && ` by ${closed.closed_by}`}
        </span>
        <div className="flex gap-2">
          {c.can_change && (
            <Button variant="danger-ghost" onClick={() => reopen.mutate(undefined)} loading={reopen.isPending}>
              Undo close-out
            </Button>
          )}
          <Button onClick={onDone}>Done</Button>
        </div>
      </div>
    </>
  );
}
