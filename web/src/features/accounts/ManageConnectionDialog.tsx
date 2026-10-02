import { ExternalLink, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";
import { Badge, Button, Dialog, FormError, Switch } from "@/components/ui";
import { api } from "@/lib/api";
import { formatMoney } from "@/lib/format";
import { AccountAvatar } from "./AccountAvatar";
import { useAccountsMutation, type Account, type Connection } from "./api";

/** Is the account followed by Viceroy (synced and counted), as opposed to left on the Bridge? */
const followed = (a: Account) => a.status !== "ignored";

/**
 * Choose which accounts shared on SimpleFIN Bridge Viceroy follows, check the Bridge for
 * banks linked since, and decide whether those are added without asking.
 */
export function ManageConnectionDialog({
  connection: c,
  accounts,
  onClose,
}: {
  connection: Connection | null;
  accounts: Account[];
  onClose: () => void;
}) {
  const mine = c ? accounts.filter((a) => a.connection_id === c.id) : [];
  const [want, setWant] = useState<Record<number, boolean>>({});
  const [warning, setWarning] = useState("");
  // Start from what's stored whenever the dialog opens.
  useEffect(() => {
    setWant({});
    setWarning("");
  }, [c?.id]);

  const on = (a: Account) => want[a.id] ?? followed(a);
  const include = mine.filter((a) => on(a) && !followed(a)).map((a) => a.id);
  const exclude = mine.filter((a) => !on(a) && followed(a)).map((a) => a.id);
  const dirty = include.length + exclude.length > 0;

  const save = useAccountsMutation(
    () => api.put<{ warning?: string }>(`/connections/${c!.id}/accounts`, { include, exclude }),
    (r) => {
      setWant({});
      if (r.warning) setWarning(r.warning);
      else onClose();
    },
  );
  const refresh = useAccountsMutation(() => api.post(`/connections/${c!.id}/sync`));
  const autoAdd = useAccountsMutation((v: boolean) => api.patch(`/connections/${c!.id}`, { auto_add_new: v }));

  // Group by bank, new offers first within each bank.
  const banks = new Map<string, Account[]>();
  for (const a of [...mine].sort((x, y) => Number(y.offered) - Number(x.offered) || x.name.localeCompare(y.name))) {
    const k = a.institution_name || "Other";
    banks.set(k, [...(banks.get(k) ?? []), a]);
  }

  return (
    <Dialog
      open={c !== null}
      onOpenChange={(o) => !o && onClose()}
      title="Manage SimpleFIN accounts"
      description="Choose which accounts shared on SimpleFIN Bridge show up in Viceroy. Turned-off accounts keep their history but are left out of balances, budgets and reports."
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {dirty ? "Cancel" : "Close"}
          </Button>
          {dirty && (
            <Button onClick={() => save.mutate(undefined)} loading={save.isPending}>
              Save
            </Button>
          )}
        </>
      }
    >
      {c && (
        <div className="flex flex-col gap-4" data-testid="manage-connection">
          {[...banks].map(([bank, list]) => (
            <section key={bank} className="flex flex-col gap-2">
              <h3 className="text-xs font-semibold uppercase tracking-wide text-muted">{bank}</h3>
              <ul className="flex flex-col divide-y divide-border rounded-lg border border-border">
                {list.map((a) => (
                  <li key={a.id} className="flex items-center gap-3 px-3 py-2.5" data-testid="manage-row">
                    <AccountAvatar account={a} size={28} />
                    <div className="min-w-0 flex-1">
                      <Switch
                        label={a.name}
                        hint={[a.mask && `••${a.mask}`, formatMoney(a.balance_cents), a.status === "disconnected" && "not shared on SimpleFIN right now"]
                          .filter(Boolean)
                          .join(" · ")}
                        checked={on(a)}
                        disabled={a.status === "review" || a.status === "closed"}
                        onCheckedChange={(v) => setWant({ ...want, [a.id]: v })}
                      />
                    </div>
                    {a.offered && <Badge tone="accent">New</Badge>}
                  </li>
                ))}
              </ul>
            </section>
          ))}
          {mine.length === 0 && <p className="text-sm text-muted">No accounts on this connection yet.</p>}
          {include.length > 0 && <p className="text-xs text-muted">Accounts you add get their last 90 days of transactions right away (one SimpleFIN request).</p>}
          {warning && <p className="rounded-lg bg-accent-soft px-3 py-2 text-[13px] text-accent">{warning}</p>}

          <div className="flex flex-col gap-3 border-t border-border pt-4">
            <p className="text-[13px] text-muted">
              Linked another bank on SimpleFIN Bridge? Check for it here, then turn it on above.
            </p>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="secondary" loading={refresh.isPending} disabled={c.requests_remaining === 0} onClick={() => refresh.mutate(undefined)}>
                {!refresh.isPending && <RefreshCw size={14} />} Check for new accounts
              </Button>
              <a
                className="inline-flex items-center gap-1.5 rounded-lg px-2 text-[13px] font-medium text-accent hover:underline"
                href="https://bridge.simplefin.org/my-account"
                target="_blank"
                rel="noreferrer"
              >
                Open SimpleFIN Bridge <ExternalLink size={13} />
              </a>
            </div>
            <Switch
              label="Add new accounts automatically"
              hint="Off: accounts that show up on the Bridge later wait here for you to add them."
              checked={c.auto_add_new}
              disabled={autoAdd.isPending}
              onCheckedChange={(v) => autoAdd.mutate(v)}
            />
          </div>
          <FormError error={save.error ?? refresh.error ?? autoAdd.error} />
        </div>
      )}
    </Dialog>
  );
}
