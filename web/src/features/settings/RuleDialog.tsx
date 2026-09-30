import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Button, CategoryPicker, Dialog, Field, FormError, Select, Switch } from "@/components/ui";
import type { Account } from "@/features/accounts/api";
import { categoriesQuery, tagsQuery, useTxnMutation } from "@/features/transactions/api";
import { api } from "@/lib/api";
import { fieldLabels, opLabels, type Rule } from "./rules";

const cents = (v: number | null) => (v === null ? "" : (v / 100).toFixed(2));

/** Create or edit a categorization rule. New rules can also be applied to existing transactions. */
export function RuleDialog({
  open,
  onOpenChange,
  rule,
  accounts,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  rule: Rule | null;
  accounts: Account[];
}) {
  const qc = useQueryClient();
  const { data: cats } = useQuery(categoriesQuery);
  const { data: tagData } = useQuery(tagsQuery);
  const [field, setField] = useState<Rule["match_field"]>("merchant");
  const [op, setOp] = useState<Rule["match_op"]>("contains");
  const [value, setValue] = useState("");
  const [accountId, setAccountId] = useState("");
  const [min, setMin] = useState("");
  const [max, setMax] = useState("");
  const [categoryId, setCategoryId] = useState<number | null>(null);
  const [merchant, setMerchant] = useState("");
  const [tag, setTag] = useState("");
  const [hide, setHide] = useState(false);
  const [applyExisting, setApplyExisting] = useState(true);
  const [applied, setApplied] = useState<number | null>(null);

  useEffect(() => {
    if (!open) return;
    setField(rule?.match_field ?? "merchant");
    setOp(rule?.match_op ?? "contains");
    setValue(rule?.match_value ?? "");
    setAccountId(rule?.account_id ? String(rule.account_id) : "");
    setMin(cents(rule?.amount_min ?? null));
    setMax(cents(rule?.amount_max ?? null));
    setCategoryId(rule?.set_category_id ?? null);
    setMerchant(rule?.set_merchant ?? "");
    setTag(tagData?.tags.find((t) => t.id === rule?.add_tag_id)?.name ?? "");
    setHide(rule?.set_hidden ?? false);
    setApplyExisting(!rule);
    setApplied(null);
  }, [open, rule, tagData]);

  const save = useTxnMutation(
    async () => {
      const body = {
        priority: rule?.priority ?? 0,
        match_field: field,
        match_op: op,
        match_value: value,
        account_id: accountId ? Number(accountId) : null,
        amount_min: min,
        amount_max: max,
        set_category_id: categoryId,
        set_merchant: merchant,
        add_tag: tag,
        set_hidden: hide,
      };
      const saved = rule ? await api.patch<Rule>(`/rules/${rule.id}`, body) : await api.post<Rule>("/rules", body);
      if (applyExisting) {
        const r = await api.post<{ updated: number }>(`/rules/${saved.id}/apply`);
        return r.updated;
      }
      return null;
    },
    (n) => {
      qc.invalidateQueries({ queryKey: ["rules"] });
      if (n === null) onOpenChange(false);
      else setApplied(n);
    },
  );

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={rule ? "Edit rule" : "New rule"}
      description="Rules run on new transactions before merchant history. The first matching rule wins."
      footer={
        applied !== null ? (
          <Button size="sm" onClick={() => onOpenChange(false)}>
            Done
          </Button>
        ) : (
          <>
            <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button size="sm" type="submit" form="rule-form" loading={save.isPending}>
              Save rule
            </Button>
          </>
        )
      }
    >
      {applied !== null ? (
        <p className="text-sm" data-testid="rule-applied">
          Rule saved and applied to {applied} existing {applied === 1 ? "transaction" : "transactions"}.
        </p>
      ) : (
        <form
          id="rule-form"
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate(undefined);
          }}
        >
          <div className="text-[11px] font-semibold uppercase tracking-wide text-muted">If</div>
          <div className="grid grid-cols-2 gap-2">
            <Select
              label="Field"
              value={field}
              onChange={(e) => setField(e.target.value as Rule["match_field"])}
              options={Object.entries(fieldLabels).map(([v, l]) => ({ value: v, label: l }))}
            />
            <Select
              label="Match"
              value={op}
              onChange={(e) => setOp(e.target.value as Rule["match_op"])}
              options={Object.entries(opLabels).map(([v, l]) => ({ value: v, label: l }))}
            />
          </div>
          <Field label="Text" value={value} onChange={(e) => setValue(e.target.value)} placeholder="e.g. shell" required />
          <Select
            label="Account"
            value={accountId}
            onChange={(e) => setAccountId(e.target.value)}
            options={[{ value: "", label: "Any account" }, ...accounts.map((a) => ({ value: String(a.id), label: a.name }))]}
          />
          <div className="grid grid-cols-2 gap-2">
            <Field label="Amount at least" inputMode="decimal" value={min} onChange={(e) => setMin(e.target.value)} placeholder="Any" />
            <Field label="Amount at most" inputMode="decimal" value={max} onChange={(e) => setMax(e.target.value)} placeholder="Any" />
          </div>

          <div className="mt-2 text-[11px] font-semibold uppercase tracking-wide text-muted">Then</div>
          <CategoryPicker label="Set category" groups={cats?.groups ?? []} value={categoryId} onChange={setCategoryId} noneLabel="Don't change" />
          <Field label="Rename merchant to" value={merchant} onChange={(e) => setMerchant(e.target.value)} placeholder="Don't rename" />
          <Field label="Add tag" value={tag} onChange={(e) => setTag(e.target.value)} placeholder="No tag" list="rule-tags" />
          <datalist id="rule-tags">
            {(tagData?.tags ?? []).map((t) => (
              <option key={t.id} value={t.name} />
            ))}
          </datalist>
          <Switch label="Hide transaction" checked={hide} onCheckedChange={setHide} />
          <div className="border-t border-border pt-3">
            <Switch
              label="Apply to existing transactions"
              hint="Categories you picked by hand are kept."
              checked={applyExisting}
              onCheckedChange={setApplyExisting}
            />
          </div>
          <FormError error={save.error} />
        </form>
      )}
    </Dialog>
  );
}
