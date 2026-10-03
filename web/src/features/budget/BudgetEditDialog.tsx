import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { Link, useNavigate } from "@tanstack/react-router";
import { Info, List as ListIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { BarChart } from "@/components/charts/BarChart";
import { Button, Dialog, Field, FormError, MoneyText, Segmented, Select, Switch, Glyph } from "@/components/ui";
import { api } from "@/lib/api";
import { formatMoney } from "@/lib/format";
import {
  centsToInput,
  historyQuery,
  monthLabel,
  shortMonth,
  useBudgetMutation,
  type BudgetLine,
  type Chunk,
  type DebtLineInfo,
  type History,
  type Target,
} from "./api";

type Kind = Chunk["kind"];

const kinds: { value: Kind; label: string }[] = [
  { value: "even", label: "Evenly" },
  { value: "day", label: "On a day" },
  { value: "week", label: "In a week" },
  { value: "every_n_weeks", label: "Every few weeks" },
];

const weekItems = [
  { value: 1, label: "1st" },
  { value: 2, label: "2nd" },
  { value: 3, label: "3rd" },
  { value: 4, label: "Last" },
];

const kindHints: Record<Kind, string> = {
  even: "Spread across the whole month.",
  day: "All at once on this day, like rent or a bill.",
  week: "During one week of the month.",
  every_n_weeks: "In equal chunks on a repeating schedule, like groceries every two weeks.",
};

const sameChunk = (a: Chunk, b: Chunk) => JSON.stringify(a) === JSON.stringify(b);

/**
 * Edit one category's (or goal's) monthly budget: amount, the past six months of spending,
 * when in the month it's usually spent, and whether the amount applies to later months.
 */
export function BudgetEditDialog({
  target,
  line,
  month,
  period,
  forwardDefault,
  onClose,
}: {
  target: Target | null;
  line: BudgetLine | null;
  month: string;
  /** The period the budget page shows; "View transactions" lists this line's within it. */
  period: { start: string; end: string } | null;
  forwardDefault: boolean;
  onClose: () => void;
}) {
  const open = !!target && !!line;
  const { data: hist } = useQuery({ ...historyQuery(target ?? { kind: "category", id: 0 }, month), enabled: open });
  const [amount, setAmount] = useState("");
  const [forward, setForward] = useState(forwardDefault);
  const [chunk, setChunk] = useState<Chunk>({ kind: "even" });
  const [timingOpen, setTimingOpen] = useState(false);
  const [hidden, setHidden] = useState(false);
  const [noPacing, setNoPacing] = useState(false);

  useEffect(() => {
    if (!line) return;
    setAmount(line.month_budget ? centsToInput(line.month_budget) : "");
    setForward(forwardDefault);
    const { no_pacing, ...when } = line.chunk;
    setChunk(when as Chunk);
    setNoPacing(!!no_pacing);
    setTimingOpen(line.chunk.kind !== "even");
    setHidden(line.hidden);
  }, [line, forwardDefault]);

  // Debt Repayment lines (a debt account, or Other) share the category's timing.
  const chunkCategory = target?.kind === "category" ? target.id : target?.kind === "account" ? target.categoryId : undefined;
  const plainCategory = target?.kind === "category" && !target.other;
  const save = useBudgetMutation(async () => {
    if (!target || !line) return;
    await api.put("/budget/amount", { [`${target.kind}_id`]: target.id, month, amount, apply_forward: forward });
    const full: Chunk = noPacing ? { ...chunk, no_pacing: true } : chunk;
    if (chunkCategory && !sameChunk(full, line.chunk)) {
      await api.put(`/budget/categories/${chunkCategory}/chunk`, full);
    }
    if (plainCategory && hidden !== line.hidden) {
      await api.put(`/budget/categories/${target.id}/hidden`, { hidden });
    }
  }, onClose);

  useEffect(() => {
    if (!open) save.reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const setKind = (k: Kind) => {
    const today = new Date().toISOString().slice(0, 10);
    setChunk(
      k === "day" ? { kind: "day", day: 1 } : k === "week" ? { kind: "week", week: 1 } : k === "every_n_weeks" ? { kind: "every_n_weeks", weeks: 2, anchor: today } : { kind: "even" },
    );
  };

  const navigate = useNavigate();
  const past = hist?.history ?? []; // six months before, then this one
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      title={
        <span className="flex items-center gap-2">
          <Glyph icon={line?.icon} />
          {line?.name}
        </span>
      }
      description={`${target?.kind === "goal" ? "Contribution" : "Budget"} for ${monthLabel(month)}`}
      footer={
        <>
          {target && period && (
            <Button
              variant="ghost"
              className="mr-auto"
              onClick={() => {
                onClose();
                navigate({ to: "/transactions" as string, search: { [target.kind]: target.id, from: period.start, to: period.end } as never });
              }}
            >
              <ListIcon size={14} /> View transactions
            </Button>
          )}
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate(undefined)} loading={save.isPending}>
            Save
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate(undefined);
        }}
      >
        <Field
          label="Monthly amount"
          inputMode="decimal"
          placeholder="0"
          value={amount}
          onChange={(e) => setAmount(e.target.value)}
          autoFocus
          data-testid="budget-amount"
        />
        {hist?.debt && <DebtSuggestions d={hist.debt} month={month} onUse={(c) => setAmount(centsToInput(c))} />}

        <div>
          <BarChart
            labels={past.map((h) => shortMonth(h.month))}
            values={past.map((h) => Math.max(0, h.actual))}
            target={past.map((h) => h.budget)}
            valueLabel={target?.kind === "goal" ? "Contributed" : target?.kind === "account" ? (hist?.debt?.mode === "paid" ? "Paid" : "Paid down") : "Spent"}
            highlight={past.length - 1}
            height={150}
          />
          <dl className="mt-2 grid grid-cols-2 gap-2 text-center">
            <div className="rounded-lg bg-surface-2 px-3 py-2">
              <dt className="text-xs text-muted">Last month</dt>
              <dd className="text-sm font-semibold" data-testid="budget-last-month">
                <MoneyText cents={hist?.last_month ?? 0} />
              </dd>
            </div>
            <div className="rounded-lg bg-surface-2 px-3 py-2">
              <dt className="text-xs text-muted">6-month average</dt>
              <dd className="text-sm font-semibold">
                <MoneyText cents={hist?.average ?? 0} />
              </dd>
            </div>
          </dl>
          {hist?.debt && <PaidBreakdown h={hist} />}
          {!!line?.rollover && (
            <p className="mt-2 text-[13px] text-muted" data-testid="budget-rollover-note">
              {line.rollover > 0 ? "Unspent" : "Overspent"} in earlier months:{" "}
              <MoneyText cents={line.rollover} className={clsx("font-medium", line.rollover < 0 && "text-negative")} />. Non-monthly categories carry it
              into this month, on top of the amount above.
            </p>
          )}
        </div>

        {chunkCategory && (
          <label className="flex items-center gap-2 text-[13px]" title="Use this if this happens randomly all at once in a month.">
            <input type="checkbox" className="size-4 accent-accent" checked={noPacing} onChange={(e) => setNoPacing(e.target.checked)} />
            <span className="font-medium">Exclude from pacing</span>
            <Info size={13} className="text-muted" aria-label="Use this if this happens randomly all at once in a month." />
          </label>
        )}
        {chunkCategory && (
          <div className="rounded-lg border border-border">
            <button
              type="button"
              className="flex w-full items-center justify-between px-3 py-2.5 text-left text-[13px]"
              onClick={() => setTimingOpen((o) => !o)}
              aria-expanded={timingOpen}
            >
              <span className="font-medium">{plainCategory ? "When is this spent?" : "When are debt payments made?"}</span>
              <span className="text-muted">{kinds.find((k) => k.value === chunk.kind)?.label}</span>
            </button>
            {timingOpen && (
              <div className="flex flex-col gap-3 border-t border-border p-3">
                <Segmented label="Spending timing" value={chunk.kind} onChange={setKind} items={kinds} />
                {chunk.kind === "day" && (
                  <Field
                    label="Day of the month"
                    type="number"
                    min={1}
                    max={31}
                    value={chunk.day}
                    onChange={(e) => setChunk({ kind: "day", day: Number(e.target.value) })}
                  />
                )}
                {chunk.kind === "week" && (
                  <Segmented label="Week of the month" value={chunk.week} onChange={(w) => setChunk({ kind: "week", week: w })} items={weekItems} />
                )}
                {chunk.kind === "every_n_weeks" && (
                  <div className="grid grid-cols-2 gap-3">
                    <Select
                      label="Every"
                      value={String(chunk.weeks)}
                      onChange={(e) => setChunk({ ...chunk, weeks: Number(e.target.value) })}
                      options={[1, 2, 3, 4].map((n) => ({ value: String(n), label: n === 1 ? "week" : `${n} weeks` }))}
                    />
                    <Field label="Starting from" type="date" value={chunk.anchor} onChange={(e) => setChunk({ ...chunk, anchor: e.target.value })} />
                  </div>
                )}
                <p className="text-xs text-muted">
                  {kindHints[chunk.kind]} Weekly and paycheck views use this to decide how much of the month's budget each period gets.
                  {!plainCategory && " Applies to every Debt Repayment line."}
                </p>
              </div>
            )}
          </div>
        )}

        <Switch label="Apply to all future months" hint={`Also use this amount after ${monthLabel(month)}.`} checked={forward} onCheckedChange={setForward} />
        {plainCategory && (
          <Switch
            label="Hide from budget"
            hint="For a category you don't use, like Water when it's included in rent. You can still pick it for transactions, and it shows up again if it has spending."
            checked={hidden}
            onCheckedChange={setHidden}
          />
        )}
        <FormError error={save.error} />
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}

const strategyName = { snowball: "Snowball", avalanche: "Avalanche" };

/** A debt's minimum and what the saved payoff plan pays on it this month, each one click to use. */
function DebtSuggestions({ d, month, onUse }: { d: DebtLineInfo; month: string; onUse: (cents: number) => void }) {
  const tile = (label: string, cents: number, hint: string, testid: string) => (
    <div className="flex items-center justify-between gap-2 rounded-lg bg-surface-2 px-3 py-2" data-testid={testid}>
      <div className="min-w-0">
        <div className="text-xs text-muted">{label}</div>
        <div className="text-sm font-semibold tabular">{formatMoney(cents)}</div>
        <div className="truncate text-[11px] text-muted">{hint}</div>
      </div>
      <Button type="button" variant="secondary" size="sm" onClick={() => onUse(cents)}>
        Use
      </Button>
    </div>
  );
  const planLink = (
    <Link to={"/reports" as string} className="font-medium text-accent hover:underline">
      Debt Free Future
    </Link>
  );
  return (
    <div className="flex flex-col gap-2" data-testid="budget-debt">
      <div className="grid gap-2 sm:grid-cols-2">
        {d.min_payment_source !== "missing" ? (
          tile("Minimum payment", d.min_payment, d.min_payment_source === "bill" ? "From your bank's statement email" : "From your statement", "budget-debt-min")
        ) : (
          <div className="rounded-lg bg-surface-2 px-3 py-2 text-xs text-muted">No minimum entered yet. Set it on {planLink}.</div>
        )}
        {d.ready && d.plan_payment !== null ? (
          tile(
            `${strategyName[d.strategy]} plan`,
            d.plan_payment,
            d.plan_payment === 0 ? "Paid off before this month" : `${formatMoney(d.extra, { whole: true })} extra a month across your debts`,
            "budget-debt-plan",
          )
        ) : (
          <div className="rounded-lg bg-surface-2 px-3 py-2 text-xs text-muted">
            {d.ready ? `The plan starts this month, so there's nothing to suggest for ${monthLabel(month)}.` : <>Enter every debt's APR and minimum on {planLink} to see what your payoff plan pays here.</>}
          </div>
        )}
      </div>
    </div>
  );
}

/** This month on a debt account: payments in, new charges, and what counts toward the budget. */
function PaidBreakdown({ h }: { h: History }) {
  const cur = h.history.at(-1);
  if (!cur || cur.paid === undefined) return null;
  const net = h.debt?.mode !== "paid";
  return (
    <p className="mt-2 text-[13px] text-muted" data-testid="budget-debt-breakdown">
      This month: paid <MoneyText cents={cur.paid} className="font-medium text-text" />
      {net && (
        <>
          {" · "}new charges <MoneyText cents={cur.charged ?? 0} className="font-medium text-text" />
          {" · "}paid down <MoneyText cents={cur.actual} className={clsx("font-medium", cur.actual < 0 ? "text-negative" : "text-text")} />
        </>
      )}
      .{" "}
      {net
        ? "Purchases already count in their own categories, so only the paydown counts here."
        : "The whole payment counts here."}{" "}
      Settings › Budget changes this.
    </p>
  );
}
