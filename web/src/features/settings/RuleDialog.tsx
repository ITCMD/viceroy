import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState, type ReactNode } from "react";
import { Button, CategoryPicker, Dialog, Field, FormError, Select, Switch, TagInput } from "@/components/ui";
import { accountLabel, type Account } from "@/features/accounts/api";
import { goalsQuery } from "@/features/goals/api";
import { categoriesQuery, tagsQuery, useTxnMutation } from "@/features/transactions/api";
import { api } from "@/lib/api";
import { fieldLabels, opLabels, type Rule, type RuleDraft } from "./rules";

const cents = (v: number | null | undefined) => (v === null || v === undefined ? "" : (v / 100).toFixed(2));
const days = Array.from({ length: 31 }, (_, i) => ({ value: String(i + 1), label: String(i + 1) }));

type Counts = { matches: number; hand_picked: number; with_hand_picked: number };

/** One condition: a checkbox that turns it on, and its inputs while on. */
function Condition({ label, on, onChange, children, testId }: { label: string; on: boolean; onChange: (v: boolean) => void; children: ReactNode; testId: string }) {
  return (
    <div className="rounded-lg border border-border p-2.5" data-testid={testId}>
      <label className="flex items-center gap-2 text-[13px] font-medium">
        <input type="checkbox" checked={on} onChange={(e) => onChange(e.target.checked)} className="accent-accent" />
        {label}
      </label>
      {on && <div className="mt-2 flex flex-col gap-2">{children}</div>}
    </div>
  );
}

/**
 * Create or edit a rule. `draft` prefills a new rule (e.g. from a transaction edit). After
 * saving, it counts the earlier transactions the rule would change and offers to apply it.
 */
export function RuleDialog({
  open,
  onOpenChange,
  rule,
  draft,
  accounts,
  title,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  rule: Rule | null;
  draft?: RuleDraft;
  accounts: Account[];
  title?: string;
}) {
  const qc = useQueryClient();
  const { data: cats } = useQuery(categoriesQuery);
  const { data: tagData } = useQuery(tagsQuery);
  const { data: goalData } = useQuery(goalsQuery);
  const [useText, setUseText] = useState(true);
  const [field, setField] = useState<Rule["match_field"]>("merchant");
  const [op, setOp] = useState<Rule["match_op"]>("contains");
  const [value, setValue] = useState("");
  const [useAccount, setUseAccount] = useState(false);
  const [accountId, setAccountId] = useState("");
  const [useAmount, setUseAmount] = useState(false);
  const [direction, setDirection] = useState<Rule["direction"]>("");
  const [min, setMin] = useState("");
  const [max, setMax] = useState("");
  const [useDays, setUseDays] = useState(false);
  const [dayMin, setDayMin] = useState("1");
  const [dayMax, setDayMax] = useState("7");
  const [categoryId, setCategoryId] = useState<number | null>(null);
  const [merchant, setMerchant] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [goalId, setGoalId] = useState("");
  const [hide, setHide] = useState(false);
  const [saved, setSaved] = useState<Rule | null>(null);
  const [counts, setCounts] = useState<Counts | null>(null);
  const [override, setOverride] = useState(false);
  const [applied, setApplied] = useState<number | null>(null);

  useEffect(() => {
    if (!open) return;
    const r: RuleDraft = rule ? { ...rule, tags: rule.tags.map((t) => t.name) } : (draft ?? {});
    setUseText(!rule || !!r.match_value);
    setField(r.match_field ?? "merchant");
    setOp(r.match_op ?? "contains");
    setValue(r.match_value ?? "");
    setUseAccount(!!r.account_id);
    setAccountId(r.account_id ? String(r.account_id) : accounts[0] ? String(accounts[0].id) : "");
    const amount = r.amount_min != null || r.amount_max != null || !!r.direction;
    setUseAmount(rule ? amount : false);
    setDirection(r.direction ?? "");
    setMin(cents(r.amount_min));
    setMax(cents(r.amount_max));
    setUseDays(r.day_min != null || r.day_max != null);
    setDayMin(String(r.day_min ?? 1));
    setDayMax(String(r.day_max ?? 7));
    setCategoryId(r.set_category_id ?? null);
    setMerchant(r.set_merchant ?? "");
    setTags(r.tags ?? []);
    setGoalId(r.set_goal_id ? String(r.set_goal_id) : "");
    setHide(r.set_hidden ?? false);
    setSaved(null);
    setCounts(null);
    setOverride(false);
    setApplied(null);
    // Prefill only when the dialog opens, not when accounts refetch.
  }, [open, rule, draft]);

  const save = useTxnMutation(
    async () => {
      const body = {
        priority: rule?.priority ?? 0,
        match_field: field,
        match_op: op,
        match_value: useText ? value : "",
        account_id: useAccount && accountId ? Number(accountId) : null,
        amount_min: useAmount ? min : "",
        amount_max: useAmount ? max : "",
        direction: useAmount ? direction : "",
        day_min: useDays ? Number(dayMin) : null,
        day_max: useDays ? Number(dayMax) : null,
        set_category_id: categoryId,
        set_merchant: merchant,
        tags,
        set_goal_id: goalId ? Number(goalId) : null,
        set_hidden: hide,
      };
      const r = rule ? await api.patch<Rule>(`/rules/${rule.id}`, body) : await api.post<Rule>("/rules", body);
      const c = await api.post<Counts>(`/rules/${r.id}/apply`, { dry_run: true });
      return { r, c };
    },
    ({ r, c }) => {
      qc.invalidateQueries({ queryKey: ["rules"] });
      setSaved(r);
      setCounts(c);
    },
  );
  const apply = useTxnMutation(
    () => api.post<{ updated: number }>(`/rules/${saved!.id}/apply`, { override_user: override }),
    (res) => setApplied(res.updated),
  );

  const goals = (goalData?.goals ?? []).filter((g) => !g.archived || g.id === Number(goalId));
  const total = counts ? (override ? counts.with_hand_picked : counts.matches) : 0;

  let body: ReactNode;
  let footer: ReactNode;
  if (saved && counts) {
    const nothing = counts.matches === 0 && counts.hand_picked === 0;
    if (applied !== null) {
      body = (
        <p className="text-sm" data-testid="rule-applied">
          Rule saved and applied to {applied} earlier {applied === 1 ? "transaction" : "transactions"}.
        </p>
      );
    } else if (nothing) {
      body = (
        <p className="text-sm" data-testid="rule-applied">
          Rule saved. No earlier transactions need changing; it will run on new ones as they arrive.
        </p>
      );
    } else {
      body = (
        <div className="flex flex-col gap-3" data-testid="rule-apply">
          <p className="text-sm">
            Rule saved. It will run on new transactions, including pending and email ones, as they arrive.
          </p>
          <p className="text-sm font-medium">
            Apply it to {total} earlier {total === 1 ? "transaction" : "transactions"} too?
          </p>
          {counts.hand_picked > 0 && (
            <Switch
              label={`Also change ${counts.hand_picked} ${counts.hand_picked === 1 ? "category" : "categories"} picked by hand`}
              hint="Includes imported history. Off keeps those categories and only applies the rest."
              checked={override}
              onCheckedChange={setOverride}
            />
          )}
          <FormError error={apply.error} />
        </div>
      );
    }
    footer =
      applied !== null || nothing ? (
        <Button size="sm" onClick={() => onOpenChange(false)}>
          Done
        </Button>
      ) : (
        <>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
            Not now
          </Button>
          <Button size="sm" loading={apply.isPending} onClick={() => apply.mutate(undefined)} disabled={total === 0}>
            Apply to earlier transactions
          </Button>
        </>
      );
  } else {
    footer = (
      <>
        <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
          Cancel
        </Button>
        <Button size="sm" type="submit" form="rule-form" loading={save.isPending}>
          Save rule
        </Button>
      </>
    );
    body = (
      <form
        id="rule-form"
        className="flex flex-col gap-2.5"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate(undefined);
        }}
      >
        <div className="text-[11px] font-semibold uppercase tracking-wide text-muted">When a transaction matches all of these</div>
        <Condition label="Merchant or statement" on={useText} onChange={setUseText} testId="rule-cond-text">
          <div className="grid grid-cols-2 gap-2">
            <Select
              label="Field"
              hideLabel
              value={field}
              onChange={(e) => setField(e.target.value as Rule["match_field"])}
              options={Object.entries(fieldLabels).map(([v, l]) => ({ value: v, label: l }))}
            />
            <Select
              label="Match"
              hideLabel
              value={op}
              onChange={(e) => setOp(e.target.value as Rule["match_op"])}
              options={Object.entries(opLabels).map(([v, l]) => ({ value: v, label: l }))}
            />
          </div>
          <Field label="Text" hideLabel value={value} onChange={(e) => setValue(e.target.value)} placeholder="e.g. shell" required={useText} />
        </Condition>
        <Condition label="Account" on={useAccount} onChange={setUseAccount} testId="rule-cond-account">
          <Select
            label="Account"
            hideLabel
            value={accountId}
            onChange={(e) => setAccountId(e.target.value)}
            options={accounts.map((a) => ({ value: String(a.id), label: accountLabel(a) }))}
          />
        </Condition>
        <Condition label="Amount" on={useAmount} onChange={setUseAmount} testId="rule-cond-amount">
          <Select
            label="Direction"
            value={direction}
            onChange={(e) => setDirection(e.target.value as Rule["direction"])}
            options={[
              { value: "", label: "Money in or out" },
              { value: "out", label: "Money out" },
              { value: "in", label: "Money in" },
            ]}
          />
          <div className="grid grid-cols-2 gap-2">
            <Field label="At least" inputMode="decimal" value={min} onChange={(e) => setMin(e.target.value)} placeholder="Any" />
            <Field label="At most" inputMode="decimal" value={max} onChange={(e) => setMax(e.target.value)} placeholder="Any" />
          </div>
          <p className="text-xs text-muted">Use the same number twice for an exact amount.</p>
        </Condition>
        <Condition label="Time of month" on={useDays} onChange={setUseDays} testId="rule-cond-days">
          <div className="grid grid-cols-2 gap-2">
            <Select label="From day" value={dayMin} onChange={(e) => setDayMin(e.target.value)} options={days} />
            <Select label="To day" value={dayMax} onChange={(e) => setDayMax(e.target.value)} options={days} />
          </div>
          {Number(dayMin) > Number(dayMax) && <p className="text-xs text-muted">Wraps around the month end.</p>}
        </Condition>

        <div className="mt-2 text-[11px] font-semibold uppercase tracking-wide text-muted">Then</div>
        <CategoryPicker label="Set category" groups={cats?.groups ?? []} value={categoryId} onChange={setCategoryId} noneLabel="Don't change" />
        <Field label="Rename merchant to" value={merchant} onChange={(e) => setMerchant(e.target.value)} placeholder="Don't rename" />
        <TagInput label="Add tags" value={tags} onChange={setTags} suggestions={(tagData?.tags ?? []).map((t) => t.name)} />
        {goals.length > 0 && (
          <Select
            label="Contribute to goal"
            value={goalId}
            onChange={(e) => setGoalId(e.target.value)}
            options={[{ value: "", label: "Don't change" }, ...goals.map((g) => ({ value: String(g.id), label: `${g.icon} ${g.name}` }))]}
          />
        )}
        <Switch label="Hide transaction" checked={hide} onCheckedChange={setHide} />
        <FormError error={save.error} />
      </form>
    );
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={saved ? "Rule saved" : (title ?? (rule ? "Edit rule" : "New rule"))}
      description={saved ? undefined : "Rules run on new transactions before merchant history. The first matching rule wins."}
      footer={footer}
    >
      {body}
    </Dialog>
  );
}
