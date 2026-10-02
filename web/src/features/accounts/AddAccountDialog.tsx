import { Landmark, PencilLine } from "lucide-react";
import { useState, type FormEvent } from "react";
import { Button, Dialog, Field, FormError, Select } from "@/components/ui";
import { api } from "@/lib/api";
import { typeLabels, useAccountsMutation } from "./api";

type Mode = "choose" | "simplefin" | "manual";

const typeOptions = Object.entries(typeLabels).map(([value, label]) => ({ value, label }));

export function AddAccountDialog({
  open,
  onOpenChange,
  onConnected,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  /** Called with the new connection so its accounts can be chosen. */
  onConnected?: (connectionID: number) => void;
}) {
  const [mode, setMode] = useState<Mode>("choose");
  const [token, setToken] = useState("");
  const [name, setName] = useState("");
  const [type, setType] = useState("checking");
  const [balance, setBalance] = useState("");
  const [syncWarning, setSyncWarning] = useState("");

  const close = () => {
    onOpenChange(false);
    setTimeout(() => {
      setMode("choose");
      setToken("");
      setName("");
      setBalance("");
      setSyncWarning("");
      connect.reset();
      manual.reset();
    }, 200);
  };

  const connect = useAccountsMutation(
    () => api.post<{ id: number; sync_error?: string }>("/connections", { setup_token: token }),
    (r) => {
      if (r.sync_error) return setSyncWarning(r.sync_error);
      close();
      onConnected?.(r.id);
    },
  );
  const manual = useAccountsMutation(() => api.post("/accounts", { name, type, balance }), close);

  const liability = ["credit_card", "loan", "mortgage", "other_liability"].includes(type);

  const body = {
    choose: (
      <div className="flex flex-col gap-2">
        <ChoiceRow icon={Landmark} title="Connect with SimpleFIN" onClick={() => setMode("simplefin")}>
          Sync balances and transactions from your banks via SimpleFIN Bridge.
        </ChoiceRow>
        <ChoiceRow icon={PencilLine} title="Add a manual account" onClick={() => setMode("manual")}>
          Track cash, property, or anything you update yourself.
        </ChoiceRow>
      </div>
    ),
    simplefin: (
      <form
        id="add-account-form"
        className="flex flex-col gap-3"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          connect.mutate(undefined);
        }}
      >
        <p className="text-[13px] text-muted">
          Create a setup token on{" "}
          <a className="text-accent underline" href="https://bridge.simplefin.org/simplefin/create" target="_blank" rel="noreferrer">
            SimpleFIN Bridge
          </a>{" "}
          and paste it here. Each token works once; Viceroy stores the resulting access key encrypted.
        </p>
        <Field label="Setup token" required value={token} onChange={(e) => setToken(e.target.value)} autoComplete="off" spellCheck={false} />
        <FormError error={connect.error} />
        {syncWarning && (
          <p className="rounded-lg bg-accent-soft px-3 py-2 text-[13px] text-accent">
            Connected, but the first sync failed: {syncWarning}. It will retry automatically.
          </p>
        )}
      </form>
    ),
    manual: (
      <form
        id="add-account-form"
        className="flex flex-col gap-3"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          manual.mutate(undefined);
        }}
      >
        <Field label="Account name" required value={name} onChange={(e) => setName(e.target.value)} />
        <Select label="Type" value={type} onChange={(e) => setType(e.target.value)} options={typeOptions} />
        <Field
          label={liability ? "Amount owed" : "Current balance"}
          inputMode="decimal"
          placeholder="0.00"
          value={balance}
          onChange={(e) => setBalance(e.target.value)}
        />
        <FormError error={manual.error} />
      </form>
    ),
  }[mode];

  const pending = connect.isPending || manual.isPending;
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => (o ? onOpenChange(true) : close())}
      title="Add account"
      footer={
        mode !== "choose" && (
          <>
            <Button variant="secondary" onClick={() => setMode("choose")} disabled={pending}>
              Back
            </Button>
            {syncWarning ? (
              <Button onClick={close}>Done</Button>
            ) : (
              <Button type="submit" form="add-account-form" loading={pending}>
                {mode === "simplefin" ? "Connect" : "Add account"}
              </Button>
            )}
          </>
        )
      }
    >
      {body}
    </Dialog>
  );
}

function ChoiceRow({ icon: Icon, title, children, onClick }: { icon: typeof Landmark; title: string; children: string; onClick: () => void }) {
  return (
    <button onClick={onClick} className="flex items-start gap-3 rounded-lg border border-border p-3 text-left transition hover:bg-surface-2">
      <span className="grid size-9 shrink-0 place-items-center rounded-full bg-accent-soft text-accent">
        <Icon size={18} />
      </span>
      <span>
        <span className="block text-sm font-medium">{title}</span>
        <span className="block text-[13px] text-muted">{children}</span>
      </span>
    </button>
  );
}
