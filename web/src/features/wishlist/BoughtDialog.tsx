import { X } from "lucide-react";
import { useEffect, useState } from "react";
import { Button, Dialog, Field, FormError, MoneyText, Segmented } from "@/components/ui";
import { shortDate, todayISO, type Transaction } from "@/features/transactions/api";
import { TxnPicker } from "@/features/transactions/TxnPicker";
import { api } from "@/lib/api";
import { useWishMutation, type WishItem } from "./api";

/** Mark an item bought: pick the purchase (spent from the Wishlist goal), or just a date. */
export function BoughtDialog({ item, onClose }: { item: WishItem | null; onClose: () => void }) {
  const [mode, setMode] = useState<"txn" | "date">("txn");
  const [txn, setTxn] = useState<Transaction | null>(null);
  const [date, setDate] = useState("");
  useEffect(() => {
    if (!item) return;
    setMode("txn");
    setTxn(null);
    setDate(todayISO());
  }, [item]);
  const save = useWishMutation(() => api.post(`/wishlist/${item!.id}/bought`, mode === "txn" ? { transaction_id: txn?.id ?? 0 } : { date }), onClose);
  useEffect(() => {
    if (!item) save.reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [item]);

  return (
    <Dialog
      open={item !== null}
      onOpenChange={(o) => !o && onClose()}
      title="Mark as bought"
      description={item?.title}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate()} loading={save.isPending} disabled={mode === "txn" && !txn}>
            Mark as bought
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3" data-testid="wish-bought-dialog">
        <Segmented
          label="How"
          value={mode}
          onChange={setMode}
          items={[
            { value: "txn", label: "Pick the purchase" },
            { value: "date", label: "Just a date" },
          ]}
        />
        {mode === "txn" ? (
          <>
            <p className="text-[13px] text-muted">The purchase is paid from the Wishlist goal: its balance goes down and it doesn't count as a contribution.</p>
            {txn ? (
              <div className="flex items-center gap-2 rounded-lg border border-border px-3 py-2 text-sm">
                <span className="min-w-0 flex-1 truncate">{txn.merchant}</span>
                <span className="text-xs text-muted">{shortDate(txn.date)}</span>
                <MoneyText cents={txn.amount_cents} colored />
                <button type="button" aria-label="Pick another transaction" className="text-muted hover:text-text" onClick={() => setTxn(null)}>
                  <X size={14} />
                </button>
              </div>
            ) : (
              <TxnPicker around={todayISO()} onPick={setTxn} outOnly />
            )}
          </>
        ) : (
          <Field label="Bought on" type="date" value={date} onChange={(e) => setDate(e.target.value)} hint="The goal's balance doesn't change." />
        )}
        <FormError error={save.error} />
      </div>
    </Dialog>
  );
}
