import { useQuery } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Button, CategoryPicker, Dialog, Field, FormError, Select, Switch } from "@/components/ui";
import { accountLabel, accountsQuery } from "@/features/accounts/api";
import { centsToInput } from "@/features/budget/api";
import { categoriesQuery } from "@/features/transactions/api";
import { cadenceLabels, inputToCents, useDeleteRecurring, useSaveRecurring, type Cadence, type RecurringInput, type RecurringSeries } from "./api";

/** What the dialog starts from: a tracked item (edit), a suggestion (confirm), a transaction
 * ("Mark as recurring") or nothing (add by hand). */
export type RecurringDraft =
  | { kind: "item"; s: RecurringSeries }
  | { kind: "suggestion"; s: RecurringSeries }
  | { kind: "transaction"; txnId: number; name: string; matchText: string; merchantId: number | null; amount: number; date: string; accountId: number; categoryId: number | null }
  | { kind: "new" };

const days = Array.from({ length: 31 }, (_, i) => ({ value: String(i + 1), label: String(i + 1) }));

export function RecurringItemDialog({ draft, onClose }: { draft: RecurringDraft | null; onClose: () => void }) {
  const open = draft !== null;
  const item = draft?.kind === "item" ? draft.s : null;
  const { data: cats } = useQuery(categoriesQuery);
  const { data: acctData } = useQuery(accountsQuery);
  const [name, setName] = useState("");
  const [matchText, setMatchText] = useState("");
  const [merchantId, setMerchantId] = useState(0);
  const [direction, setDirection] = useState<"out" | "in">("out");
  const [amount, setAmount] = useState("");
  const [varies, setVaries] = useState(false);
  const [cadence, setCadence] = useState<Cadence>("monthly");
  const [anchor, setAnchor] = useState("");
  const [day2, setDay2] = useState("15");
  const [accountId, setAccountId] = useState(0);
  const [categoryId, setCategoryId] = useState<number | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [localError, setLocalError] = useState("");

  useEffect(() => {
    if (!draft) return;
    setConfirmDelete(false);
    setLocalError("");
    if (draft.kind === "item" || draft.kind === "suggestion") {
      const s = draft.s;
      setName(s.name);
      setMatchText(draft.kind === "item" ? s.match_text : s.merchant_id ? "" : s.name);
      setMerchantId(s.merchant_id);
      setDirection(s.amount < 0 ? "out" : "in");
      setAmount(centsToInput(Math.abs(s.amount)));
      setVaries(s.variable);
      setCadence(s.cadence);
      setAnchor(s.anchor_date || s.next_date);
      setDay2(String(s.day2 || 15));
      setAccountId(s.account_id);
      setCategoryId(s.category_id || null);
    } else if (draft.kind === "transaction") {
      setName(draft.name);
      setMatchText(draft.merchantId ? "" : draft.matchText);
      setMerchantId(draft.merchantId ?? 0);
      setDirection(draft.amount < 0 ? "out" : "in");
      setAmount(centsToInput(Math.abs(draft.amount)));
      setVaries(false);
      setCadence("monthly");
      setAnchor(nextMonthly(draft.date));
      setDay2("15");
      setAccountId(draft.accountId);
      setCategoryId(draft.categoryId);
    } else {
      setName("");
      setMatchText("");
      setMerchantId(0);
      setDirection("out");
      setAmount("");
      setVaries(false);
      setCadence("monthly");
      setAnchor("");
      setDay2("15");
      setAccountId(0);
      setCategoryId(null);
    }
  }, [draft]);

  const save = useSaveRecurring(item?.id ?? null, onClose);
  const del = useDeleteRecurring(onClose);
  useEffect(() => {
    if (!open) {
      save.reset();
      del.reset();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const submit = () => {
    const cents = inputToCents(amount);
    if (!Number.isFinite(cents) || cents <= 0) {
      setLocalError("Enter an amount.");
      return;
    }
    setLocalError("");
    const body: RecurringInput = {
      name,
      match_text: matchText,
      merchant_id: merchantId,
      amount: direction === "out" ? -cents : cents,
      amount_varies: varies,
      cadence,
      anchor_date: anchor,
      day2: cadence === "semimonthly" ? Number(day2) : 0,
      account_id: accountId,
      category_id: categoryId ?? 0,
    };
    if (draft?.kind === "suggestion") body.series_key = draft.s.key;
    if (draft?.kind === "transaction") body.transaction_id = draft.txnId;
    save.mutate(body);
  };

  const accounts = (acctData?.accounts ?? []).filter((a) => a.status !== "ignored" || a.id === accountId);
  const title = item ? "Edit recurring" : draft?.kind === "suggestion" ? "Track recurring" : "New recurring";

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      title={title}
      description={draft?.kind === "suggestion" ? "Viceroy noticed this repeating. Check the details, then save to track it." : undefined}
      footer={
        <>
          {item &&
            (confirmDelete ? (
              <Button variant="danger" className="mr-auto" loading={del.isPending} onClick={() => del.mutate(item.id)}>
                Stop tracking
              </Button>
            ) : (
              <Button variant="danger-ghost" className="mr-auto" aria-label="Stop tracking" onClick={() => setConfirmDelete(true)}>
                <Trash2 size={14} />
              </Button>
            ))}
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={submit} loading={save.isPending}>
            Save
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
        data-testid="recurring-dialog"
      >
        <Field label="Name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Rent" autoFocus={draft?.kind === "new"} />
        <div className="grid grid-cols-[auto_1fr] gap-3">
          <Select
            label="Direction"
            value={direction}
            onChange={(e) => setDirection(e.target.value as "out" | "in")}
            options={[
              { value: "out", label: "Money out" },
              { value: "in", label: "Money in" },
            ]}
          />
          <Field label="Amount" inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} placeholder="15.99" />
        </div>
        <Switch label="Amount varies" hint="Like a utility bill. Any amount from this merchant counts as the payment." checked={varies} onCheckedChange={setVaries} />
        <div className="grid grid-cols-2 gap-3">
          <Select
            label="Repeats"
            value={cadence}
            onChange={(e) => setCadence(e.target.value as Cadence)}
            options={Object.entries(cadenceLabels).map(([value, label]) => ({ value, label }))}
          />
          <Field label="Next due" type="date" value={anchor} onChange={(e) => setAnchor(e.target.value)} />
        </div>
        {cadence === "semimonthly" && <Select label="Also due on day" value={day2} onChange={(e) => setDay2(e.target.value)} options={days} />}
        <Select
          label="Account"
          value={String(accountId)}
          onChange={(e) => setAccountId(Number(e.target.value))}
          options={[{ value: "0", label: "Any account" }, ...accounts.map((a) => ({ value: String(a.id), label: accountLabel(a) }))]}
        />
        <CategoryPicker label="Category" groups={cats?.groups ?? []} value={categoryId} onChange={setCategoryId} noneLabel="No category" />
        <div>
          <Field
            label="Matches transactions containing"
            value={matchText}
            onChange={(e) => setMatchText(e.target.value)}
            placeholder={merchantId ? "Matched by merchant" : "OAK APTS"}
            hint={
              merchantId
                ? `Payments from ${name || "this merchant"} count automatically. Add text to also match other bank wording.`
                : "Text from the merchant or the bank statement. Payments with it count toward this item."
            }
          />
          {merchantId > 0 && item && (
            <button type="button" className="mt-1 text-xs text-muted hover:text-text" onClick={() => setMerchantId(0)}>
              Match by text only
            </button>
          )}
        </div>
        {confirmDelete && <p className="text-[13px] text-negative">It stops showing as upcoming. Transactions aren't changed.</p>}
        <FormError error={localError || save.error || del.error} />
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}

/** The same day next month, clamped (a monthly charge from Jan 31 is next due Feb 28). */
function nextMonthly(date: string) {
  const [y, m, d] = date.split("-").map(Number);
  const last = new Date(Date.UTC(y, m + 1, 0)).getUTCDate();
  const next = new Date(Date.UTC(y, m, Math.min(d, last)));
  return next.toISOString().slice(0, 10);
}
