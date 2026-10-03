import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { Trash2, X } from "lucide-react";
import { useEffect, useState } from "react";
import { Button, Dialog, Field, FormError, MoneyText, IconPicker } from "@/components/ui";
import { shortDate } from "@/features/transactions/api";
import { TxnPicker } from "@/features/transactions/TxnPicker";
import { api } from "@/lib/api";

export type NetWorthNote = {
  id: number;
  date: string;
  label: string;
  icon: string;
  transaction_id: number;
  txn_name: string;
  txn_amount: number;
  txn_date: string;
};

export const netWorthNotesQuery = queryOptions({
  queryKey: ["networth", "notes"],
  queryFn: () => api.get<{ annotations: NetWorthNote[] }>("/networth/annotations"),
});

const suggestions = ["🏠", "🚗", "💍", "👶", "🎓", "💼", "✈️", "📈", "📉", "🏥", "🎉", "💸"];

/** What the dialog edits: an existing note, or a new one on a day. */
export type NoteDraft = { note: NetWorthNote } | { date: string };

export function NetWorthNoteDialog({ draft, onClose }: { draft: NoteDraft | null; onClose: () => void }) {
  const note = draft && "note" in draft ? draft.note : null;
  const [date, setDate] = useState("");
  const [label, setLabel] = useState("");
  const [icon, setIcon] = useState("");
  const [txn, setTxn] = useState<{ id: number; name: string; amount: number; date: string } | null>(null);
  const [picking, setPicking] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  useEffect(() => {
    if (!draft) return;
    setDate(note?.date ?? ("date" in draft ? draft.date : ""));
    setLabel(note?.label ?? "");
    setIcon(note?.icon ?? "");
    setTxn(note?.transaction_id ? { id: note.transaction_id, name: note.txn_name, amount: note.txn_amount, date: note.txn_date } : null);
    setPicking(false);
    setConfirmDelete(false);
  }, [draft, note]);

  const qc = useQueryClient();
  const refresh = () => qc.invalidateQueries({ queryKey: ["networth", "notes"] });
  const save = useMutation({
    mutationFn: () => {
      const body = { date, label, icon, transaction_id: txn?.id ?? 0 };
      return note ? api.patch(`/networth/annotations/${note.id}`, body) : api.post("/networth/annotations", body);
    },
    onSuccess: onClose,
    onSettled: refresh,
  });
  const del = useMutation({ mutationFn: () => api.del(`/networth/annotations/${note!.id}`), onSuccess: onClose, onSettled: refresh });
  useEffect(() => {
    if (!draft) {
      save.reset();
      del.reset();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft]);

  return (
    <Dialog
      open={draft !== null}
      onOpenChange={(o) => !o && onClose()}
      title={note ? "Edit note" : "Add a note"}
      description="Shows on the net worth chart. Hover its icon to see the name."
      footer={
        <>
          {note &&
            (confirmDelete ? (
              <Button variant="danger" className="mr-auto" loading={del.isPending} onClick={() => del.mutate()}>
                Delete note
              </Button>
            ) : (
              <Button variant="danger-ghost" className="mr-auto" aria-label="Delete note" onClick={() => setConfirmDelete(true)}>
                <Trash2 size={14} />
              </Button>
            ))}
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate()} loading={save.isPending}>
            Save
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
        data-testid="networth-note-dialog"
      >
        <div className="grid grid-cols-[4rem_1fr] gap-3">
          <IconPicker value={icon} onChange={setIcon} placeholder="📌" />
          <Field label="Name" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Bought a car" autoFocus />
        </div>
        <div className="flex flex-wrap gap-1" aria-label="Icon suggestions">
          {suggestions.map((s) => (
            <button
              key={s}
              type="button"
              onClick={() => setIcon(icon === s ? "" : s)}
              className={clsx("grid size-8 place-items-center rounded-md text-base hover:bg-surface-2", icon === s && "bg-accent-soft ring-1 ring-accent")}
            >
              {s}
            </button>
          ))}
        </div>
        <Field label="Date" type="date" value={date} onChange={(e) => setDate(e.target.value)} />
        <div className="flex flex-col gap-1">
          <div className="text-[13px] font-medium">Linked transaction</div>
          {txn ? (
            <div className="flex items-center gap-2 rounded-lg border border-border px-3 py-2 text-sm">
              <span className="min-w-0 flex-1 truncate">{txn.name}</span>
              <span className="text-xs text-muted">{shortDate(txn.date)}</span>
              <MoneyText cents={txn.amount} colored />
              <button type="button" aria-label="Remove linked transaction" className="text-muted hover:text-text" onClick={() => setTxn(null)}>
                <X size={14} />
              </button>
            </div>
          ) : picking ? (
            <TxnPicker
              around={date}
              onPick={(t) => {
                setTxn({ id: t.id, name: t.merchant, amount: t.amount_cents, date: t.date });
                setPicking(false);
              }}
            />
          ) : (
            <div>
              <Button size="sm" variant="secondary" onClick={() => setPicking(true)}>
                Link a transaction
              </Button>
            </div>
          )}
        </div>
        <FormError error={save.error ?? del.error} />
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}
