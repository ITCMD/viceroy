import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { BarChart } from "@/components/charts/BarChart";
import { Button, Dialog, Field, FormError, MoneyText, Segmented, Select, Switch } from "@/components/ui";
import { api } from "@/lib/api";
import {
  centsToInput,
  historyQuery,
  monthLabel,
  shortMonth,
  useBudgetMutation,
  type BudgetLine,
  type Chunk,
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
  forwardDefault,
  onClose,
}: {
  target: Target | null;
  line: BudgetLine | null;
  month: string;
  forwardDefault: boolean;
  onClose: () => void;
}) {
  const open = !!target && !!line;
  const { data: hist } = useQuery({ ...historyQuery(target ?? { kind: "category", id: 0 }, month), enabled: open });
  const [amount, setAmount] = useState("");
  const [forward, setForward] = useState(forwardDefault);
  const [chunk, setChunk] = useState<Chunk>({ kind: "even" });
  const [timingOpen, setTimingOpen] = useState(false);

  useEffect(() => {
    if (!line) return;
    setAmount(line.month_budget ? centsToInput(line.month_budget) : "");
    setForward(forwardDefault);
    setChunk(line.chunk);
    setTimingOpen(line.chunk.kind !== "even");
  }, [line, forwardDefault]);

  const save = useBudgetMutation(async () => {
    if (!target || !line) return;
    await api.put("/budget/amount", { [`${target.kind}_id`]: target.id, month, amount, apply_forward: forward });
    if (target.kind === "category" && !sameChunk(chunk, line.chunk)) {
      await api.put(`/budget/categories/${target.id}/chunk`, chunk);
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

  const past = hist?.history ?? []; // six months before, then this one
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      title={
        <span className="flex items-center gap-2">
          {line?.icon && <span aria-hidden>{line.icon}</span>}
          {line?.name}
        </span>
      }
      description={`${target?.kind === "goal" ? "Contribution" : "Budget"} for ${monthLabel(month)}`}
      footer={
        <>
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

        <div>
          <BarChart
            labels={past.map((h) => shortMonth(h.month))}
            values={past.map((h) => Math.max(0, h.actual))}
            target={past.map((h) => h.budget)}
            valueLabel={target?.kind === "goal" ? "Contributed" : "Spent"}
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
        </div>

        {target?.kind === "category" && (
          <div className="rounded-lg border border-border">
            <button
              type="button"
              className="flex w-full items-center justify-between px-3 py-2.5 text-left text-[13px]"
              onClick={() => setTimingOpen((o) => !o)}
              aria-expanded={timingOpen}
            >
              <span className="font-medium">When is this spent?</span>
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
                <p className="text-xs text-muted">{kindHints[chunk.kind]} Weekly and paycheck views use this to decide how much of the month's budget each period gets.</p>
              </div>
            )}
          </div>
        )}

        <Switch label="Apply to all future months" hint={`Also use this amount after ${monthLabel(month)}.`} checked={forward} onCheckedChange={setForward} />
        <FormError error={save.error} />
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}
