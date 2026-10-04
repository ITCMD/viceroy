import { useMutation, useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { CircleCheck, CircleX, TriangleAlert } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { Button, CategoryPicker, Dialog, Field, FormError } from "@/components/ui";
import { categoriesQuery } from "@/features/transactions/api";
import { api } from "@/lib/api";
import { formatMoney } from "@/lib/format";

type Verdict = {
  answer: "yes" | "careful" | "no";
  headline: string;
  details: string[];
  budget: number;
  spent: number;
  after: number;
  planned: number;
  upcoming: number;
  left: number;
};

type Result = {
  category: { id: number; name: string; icon: string };
  amount_cents: number;
  amount_source: "you" | "history" | "estimate";
  history_count: number;
  verdict: Verdict;
};

const looks = {
  yes: { icon: CircleCheck, tone: "border-positive/30 bg-positive/10 text-positive", label: "Go for it" },
  careful: { icon: TriangleAlert, tone: "border-accent/40 bg-accent-soft text-accent", label: "Yes, but watch it" },
  no: { icon: CircleX, tone: "border-negative/30 bg-negative/10 text-negative", label: "Better not" },
};

const examples = ["Lunch out", "Coffee", "Bagels from the bagel shop", "A grocery run"];

/** "Can I buy it?": describe a purchase, get yes / careful / no from the budget and pacing. */
export function CanIBuyDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const { data: cats } = useQuery(categoriesQuery);
  const [text, setText] = useState("");
  const [amount, setAmount] = useState("");
  const [category, setCategory] = useState<number | null>(null);
  const check = useMutation({
    mutationFn: (body: { text: string; amount: string; category_id: number }) => api.post<Result>("/can-i-buy", body),
    onSuccess: (r) => {
      // Show what was used, so a re-check with a tweak doesn't need the AI again.
      setCategory(r.category.id);
      setAmount((r.amount_cents / 100).toFixed(2));
    },
  });

  useEffect(() => {
    if (open) return;
    setText("");
    setAmount("");
    setCategory(null);
    check.reset();
  }, [open]);

  const submit = (e?: FormEvent, t = text) => {
    e?.preventDefault();
    if (!t.trim() && !(category && amount)) return;
    check.mutate({ text: t.trim(), amount: amount.trim(), category_id: category ?? 0 });
  };
  const r = check.data;
  // A new description starts over: the AI picks the category and price again.
  const describe = (t: string) => {
    setText(t);
    if (r) {
      setCategory(null);
      setAmount("");
      check.reset();
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Can I buy it?" description="Checks it against this month's budget and your pace so far.">
      <form className="flex flex-col gap-3" onSubmit={submit}>
        <div className="flex items-end gap-2">
          <Field
            label="What do you want to buy?"
            className="flex-1"
            placeholder="Bagels from the bagel shop"
            value={text}
            onChange={(e) => describe(e.target.value)}
            autoFocus
            maxLength={300}
          />
          <Field label="Price" placeholder="Optional" inputMode="decimal" className="w-24" value={amount} onChange={(e) => setAmount(e.target.value)} />
        </div>
        {!r && !text && (
          <div className="flex flex-wrap gap-1.5">
            {examples.map((x) => (
              <button
                key={x}
                type="button"
                className="rounded-full border border-border px-2.5 py-1 text-[12px] text-muted hover:bg-surface-2 hover:text-text"
                onClick={() => {
                  describe(x);
                  submit(undefined, x);
                }}
              >
                {x}
              </button>
            ))}
          </div>
        )}
        {r && (
          <CategoryPicker label="Category" groups={cats?.groups ?? []} value={category} allowNone={false} onChange={(id) => setCategory(id)} />
        )}
        <Button type="submit" loading={check.isPending} disabled={!text.trim() && !(category && amount)}>
          {r ? "Check again" : "Check"}
        </Button>
        <FormError error={check.error} />
      </form>
      {r && <Answer r={r} />}
    </Dialog>
  );
}

function Answer({ r }: { r: Result }) {
  const v = r.verdict;
  const { icon: Icon, tone, label } = looks[v.answer];
  const scale = Math.max(v.budget, v.after + v.upcoming, 1);
  const pct = (c: number) => `${Math.min(100, (c / scale) * 100)}%`;
  const source =
    r.amount_source === "history"
      ? `your usual, from ${r.history_count} past purchases`
      : r.amount_source === "estimate"
        ? "an AI estimate; enter the real price for a better answer"
        : "the price you entered";
  return (
    <div className="mt-4 flex flex-col gap-3" data-testid="can-i-buy-answer" data-answer={v.answer}>
      <div className={clsx("flex gap-3 rounded-xl border p-3", tone)}>
        <Icon size={20} className="mt-0.5 shrink-0" />
        <div className="min-w-0">
          <p className="text-[13px] font-semibold uppercase tracking-wide">{label}</p>
          <p className="mt-0.5 text-sm font-medium text-text">{v.headline}</p>
          {v.details.map((d) => (
            <p key={d} className="mt-1 text-[13px] text-muted">
              {d}
            </p>
          ))}
        </div>
      </div>
      <div>
        <div className="flex items-baseline justify-between text-[13px]">
          <span className="font-medium">{r.category.name}</span>
          <span className="text-muted">
            {formatMoney(v.after)} of {formatMoney(v.budget)} after this
          </span>
        </div>
        <div className="relative mt-1.5 h-2 rounded-full bg-surface-2" aria-hidden>
          <span className="absolute inset-y-0 left-0 rounded-l-full bg-accent" style={{ width: pct(v.spent) }} />
          <span
            className={clsx("absolute inset-y-0", v.left < 0 ? "bg-negative" : "bg-accent/45")}
            style={{ left: pct(v.spent), width: `calc(${pct(v.after)} - ${pct(v.spent)})` }}
          />
          {v.upcoming > 0 && (
            <span className="absolute inset-y-0 bg-upcoming/70" style={{ left: pct(v.after), width: `calc(${pct(v.after + v.upcoming)} - ${pct(v.after)})` }} />
          )}
          {v.budget > 0 && v.planned > 0 && v.planned < v.budget && (
            <span className="absolute -inset-y-0.5 w-0.5 rounded bg-text/50" style={{ left: pct(v.planned) }} />
          )}
          {v.budget > 0 && v.budget < scale && <span className="absolute -inset-y-1 w-0.5 rounded bg-negative" style={{ left: pct(v.budget) }} />}
        </div>
        <p className="mt-1.5 text-xs text-muted">
          Spent {formatMoney(v.spent)} · this purchase {formatMoney(r.amount_cents)}
          {v.upcoming > 0 && <> · {formatMoney(v.upcoming)} recurring still due</>}
          {v.planned > 0 && <> · tick = planned by end of week ({formatMoney(v.planned)})</>}
        </p>
        <p className="mt-1 text-xs text-muted">Price: {source}.</p>
      </div>
    </div>
  );
}
