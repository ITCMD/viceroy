import { useQuery } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
import { useEffect, useState } from "react";
import { Button, CategoryPicker, Dialog, Field, FormError, MoneyText, Segmented, Select, TagInput, TextArea } from "@/components/ui";
import { accountLabel, type Account } from "@/features/accounts/api";
import { api, ApiError } from "@/lib/api";
import { categoriesQuery, shortDate, tagsQuery, todayISO, useTxnMutation, type Transaction, type TxnSummary } from "./api";

type Kind = "standalone" | "pending";

/** "+" dialog: a standalone transaction (cash, missing ones) or a pending entry that links up when it posts. */
export function AddTransactionDialog({
  open,
  onOpenChange,
  accounts,
  defaultAccount,
  initial,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  accounts: Account[];
  defaultAccount?: number;
  /** Prefill (e.g. from a bank email). */
  initial?: { date?: string; description?: string; amount?: string };
  onCreated: (id: number) => void;
}) {
  const [kind, setKind] = useState<Kind>("pending");
  const [accountId, setAccountId] = useState("");
  const [date, setDate] = useState(todayISO());
  const [description, setDescription] = useState("");
  const [amount, setAmount] = useState("");
  const [direction, setDirection] = useState<"out" | "in">("out");
  const [categoryId, setCategoryId] = useState<number | null>(null);
  const [notes, setNotes] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [duplicates, setDuplicates] = useState<TxnSummary[] | null>(null);
  const { data: cats } = useQuery(categoriesQuery);
  const { data: tagData } = useQuery(tagsQuery);

  const paperCash = accounts.find((a) => a.builtin === "paper_cash");
  const bankDefault = defaultAccount ?? accounts.find((a) => !a.builtin)?.id ?? accounts[0]?.id;
  const chooseKind = (k: Kind) => {
    setKind(k);
    // Standalone entries are usually cash; pending entries are waiting on a bank.
    if (!defaultAccount) setAccountId(String((k === "standalone" ? paperCash?.id : undefined) ?? bankDefault ?? ""));
  };

  useEffect(() => {
    if (!open) return;
    setKind("pending");
    setAccountId(String(bankDefault ?? ""));
    setDate(initial?.date || todayISO());
    setDescription(initial?.description ?? "");
    setAmount(initial?.amount ?? "");
    setDirection("out");
    setCategoryId(null);
    setNotes("");
    setTags([]);
    setDuplicates(null);
  }, [open]);

  const create = useTxnMutation(
    async (force: boolean) => {
      const clean = amount.trim().replace(/^[-+]/, "");
      try {
        return await api.post<Transaction>("/transactions", {
          account_id: Number(accountId),
          date,
          amount: (direction === "out" ? "-" : "") + clean,
          description,
          category_id: categoryId,
          notes,
          tags,
          pending: kind === "pending",
          force,
        });
      } catch (e) {
        if (e instanceof ApiError && e.status === 409) {
          setDuplicates(e.data.duplicates as TxnSummary[]);
          return null;
        }
        throw e;
      }
    },
    (t) => {
      if (!t) return;
      onOpenChange(false);
      onCreated(t.id);
    },
  );

  const submit = (force: boolean) => create.mutate(force);

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Add transaction"
      footer={
        duplicates ? (
          <>
            <Button variant="ghost" size="sm" onClick={() => setDuplicates(null)}>
              Go back
            </Button>
            <Button size="sm" loading={create.isPending} onClick={() => submit(true)}>
              Add anyway
            </Button>
          </>
        ) : (
          <>
            <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button size="sm" type="submit" form="add-txn" loading={create.isPending}>
              {kind === "pending" ? "Add pending entry" : "Add transaction"}
            </Button>
          </>
        )
      }
    >
      {duplicates ? (
        <div className="flex flex-col gap-3" data-testid="duplicate-warning">
          <div className="flex gap-2 rounded-lg bg-accent-soft px-3 py-2 text-[13px] text-accent">
            <AlertTriangle size={16} className="mt-0.5 shrink-0" />
            <span>This looks like it already posted to the account. Add the pending entry anyway?</span>
          </div>
          <ul className="divide-y divide-border rounded-lg border border-border">
            {duplicates.map((d) => (
              <li key={d.id} className="flex items-center justify-between gap-3 px-3 py-2 text-sm">
                <span className="min-w-0">
                  <span className="block truncate font-medium">{d.merchant_name || d.description}</span>
                  <span className="block text-xs text-muted">{shortDate(d.date)}</span>
                </span>
                <MoneyText cents={d.amount_cents} colored />
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <form
          id="add-txn"
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            submit(false);
          }}
        >
          <Segmented
            label="Kind"
            value={kind}
            onChange={chooseKind}
            items={[
              { value: "pending", label: "Pending entry" },
              { value: "standalone", label: "Standalone" },
            ]}
          />
          <p className="-mt-1 text-xs text-muted">
            {kind === "pending"
              ? "Counts toward your budget now, then links to the bank's transaction when it arrives."
              : paperCash
                ? "For cash (Paper Cash) or anything that will never come from the bank."
                : "For anything that will never come from the bank."}
          </p>
          <Select
            label="Account"
            value={accountId}
            onChange={(e) => setAccountId(e.target.value)}
            options={accounts.map((a) => ({ value: String(a.id), label: accountLabel(a) }))}
          />
          <Field label="Merchant" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="e.g. Chipotle" required />
          <div className="grid grid-cols-[1fr_auto] items-end gap-2">
            <Field label="Amount" inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} placeholder="0.00" required />
            <Segmented
              label="Direction"
              className="h-9 items-center"
              value={direction}
              onChange={setDirection}
              items={[
                { value: "out", label: "Expense" },
                { value: "in", label: "Income" },
              ]}
            />
          </div>
          <Field label="Date" type="date" value={date} onChange={(e) => setDate(e.target.value)} required />
          <CategoryPicker label="Category" groups={cats?.groups ?? []} value={categoryId} onChange={setCategoryId} noneLabel="Auto (rules and history)" />
          <TagInput label="Tags" value={tags} onChange={setTags} suggestions={(tagData?.tags ?? []).map((t) => t.name)} />
          <TextArea label="Notes" value={notes} onChange={(e) => setNotes(e.target.value)} rows={2} />
          <FormError error={create.error} />
        </form>
      )}
    </Dialog>
  );
}
