import { useEffect, useState } from "react";
import { Badge, Button, FormError, Field, MoneyText, Select, Sheet, Switch } from "@/components/ui";
import { api } from "@/lib/api";
import { timeAgo } from "@/lib/format";
import { StatusBadge } from "./StatusBadge";
import { accountSubtitle, typeLabels, useAccountsMutation, type Account } from "./api";

const typeOptions = Object.entries(typeLabels).map(([value, label]) => ({ value, label }));

/** Account details and settings: rename, type, net worth, hide, close, merge, delete. */
export function AccountSheet({ account, accounts, onClose }: { account: Account | null; accounts: Account[]; onClose: () => void }) {
  const [name, setName] = useState("");
  const [type, setType] = useState("");
  const [balance, setBalance] = useState("");
  const [mergeInto, setMergeInto] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => {
    if (!account) return;
    setName(account.name);
    setType(account.type);
    setBalance(((account.is_liability ? -1 : 1) * account.balance_cents / 100).toFixed(2));
    setMergeInto("");
    setConfirmDelete(false);
  }, [account]);

  const patch = useAccountsMutation((body: Record<string, unknown>) => api.patch(`/accounts/${account!.id}`, body));
  const merge = useAccountsMutation(() => api.post("/accounts/merge", { from: account!.id, into: Number(mergeInto) }), onClose);
  const del = useAccountsMutation(() => api.del(`/accounts/${account!.id}`), onClose);

  if (!account) return <Sheet open={false} onOpenChange={onClose} title="" children={null} />;
  const others = accounts.filter((a) => a.id !== account.id && a.status !== "review");
  const dirty = name !== account.name || type !== account.type || (account.is_manual && balance !== ((account.is_liability ? -1 : 1) * account.balance_cents / 100).toFixed(2));

  return (
    <Sheet open onOpenChange={(o) => !o && onClose()} title={account.name}>
      <div className="flex flex-col gap-6">
        <div>
          <MoneyText cents={account.balance_cents} className="text-2xl font-semibold" />
          <div className="mt-1 flex flex-wrap items-center gap-2 text-[13px] text-muted">
            <span>{accountSubtitle(account)}</span>
            <StatusBadge account={account} />
            {account.hidden && <Badge>Hidden</Badge>}
          </div>
          {!account.is_manual && <div className="mt-1 text-xs text-muted">Last synced {timeAgo(account.last_synced_at)}</div>}
          {account.provider_name && account.provider_name !== account.name && (
            <div className="mt-1 text-xs text-muted">Bank name: {account.provider_name}</div>
          )}
        </div>

        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            patch.mutate({ name, type, ...(account.is_manual ? { balance } : {}) });
          }}
        >
          <Field label="Name" value={name} onChange={(e) => setName(e.target.value)} required />
          {!account.builtin && <Select label="Type" value={type} onChange={(e) => setType(e.target.value)} options={typeOptions} />}
          {account.is_manual && (
            <Field
              label={account.is_liability ? "Amount owed" : "Balance"}
              inputMode="decimal"
              value={balance}
              onChange={(e) => setBalance(e.target.value)}
              hint="Transactions you add to this account adjust it automatically."
            />
          )}
          <FormError error={patch.error} />
          <div>
            <Button type="submit" size="sm" disabled={!dirty} loading={patch.isPending}>
              Save changes
            </Button>
          </div>
        </form>

        <div className="flex flex-col gap-3 border-t border-border pt-5">
          <Switch
            label="Include in net worth"
            checked={account.include_in_net_worth}
            onCheckedChange={(v) => patch.mutate({ include_in_net_worth: v })}
          />
          <Switch label="Hide from lists" checked={account.hidden} onCheckedChange={(v) => patch.mutate({ hidden: v })} />
          {account.builtin ? (
            <p className="text-xs text-muted">Paper Cash is built in. Turn it off in Settings if you don't track cash.</p>
          ) : (
            <Switch
              label="Account is closed"
              hint="Keeps its history but stops showing it as active."
              checked={account.status === "closed"}
              onCheckedChange={(v) => patch.mutate({ closed: v })}
            />
          )}
        </div>

        {!account.builtin && others.length > 0 && (
          <div className="flex flex-col gap-2 border-t border-border pt-5">
            <Select
              label="Merge into another account"
              value={mergeInto}
              onChange={(e) => setMergeInto(e.target.value)}
              options={[{ value: "", label: "Choose an account…" }, ...others.map((o) => ({ value: String(o.id), label: `${o.name} (${accountSubtitle(o)})` }))]}
            />
            <p className="text-xs text-muted">
              Moves this account's transactions and balance history into the chosen account (duplicates are dropped), then removes this one.
            </p>
            <FormError error={merge.error} />
            <div>
              <Button size="sm" variant="secondary" disabled={!mergeInto} loading={merge.isPending} onClick={() => merge.mutate(undefined)}>
                Merge
              </Button>
            </div>
          </div>
        )}

        {!account.builtin && <div className="flex flex-col gap-2 border-t border-border pt-5">
          <FormError error={del.error} />
          {confirmDelete ? (
            <div className="flex items-center gap-2">
              <span className="text-[13px]">Delete this account and all its transactions?</span>
              <Button size="sm" variant="danger" loading={del.isPending} onClick={() => del.mutate(undefined)}>
                Delete
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setConfirmDelete(false)}>
                Cancel
              </Button>
            </div>
          ) : (
            <div>
              <Button size="sm" variant="danger-ghost" onClick={() => setConfirmDelete(true)}>
                Delete account
              </Button>
            </div>
          )}
        </div>}
      </div>
    </Sheet>
  );
}
