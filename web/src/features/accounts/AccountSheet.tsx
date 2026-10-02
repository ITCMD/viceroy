import { useQuery } from "@tanstack/react-query";
import { Sparkles, Upload } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Badge, Button, FormError, Field, MoneyText, Select, Sheet, Switch } from "@/components/ui";
import { api } from "@/lib/api";
import { timeAgo } from "@/lib/format";
import { shrinkImage } from "@/lib/image";
import { AccountAvatar } from "./AccountAvatar";
import { AccountHistory } from "./AccountHistory";
import { BillBadges } from "./BillBadges";
import { StatusBadge } from "./StatusBadge";
import { householdQuery } from "@/features/household/api";
import { accountSubtitle, typeLabels, useAccountsMutation, type Account } from "./api";

const typeOptions = Object.entries(typeLabels).map(([value, label]) => ({ value, label }));

/** Account details and settings: rename, type, net worth, hide, close, merge, delete. */
export function AccountSheet({ account, accounts, onClose }: { account: Account | null; accounts: Account[]; onClose: () => void }) {
  const [name, setName] = useState("");
  const [type, setType] = useState("");
  const [balance, setBalance] = useState("");
  const [mergeInto, setMergeInto] = useState("");
  const [replaceWith, setReplaceWith] = useState("");
  const [replaceWarning, setReplaceWarning] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => {
    if (!account) return;
    setName(account.name);
    setType(account.type);
    setBalance(((account.is_liability ? -1 : 1) * account.balance_cents / 100).toFixed(2));
    setMergeInto("");
    setReplaceWith("");
    setReplaceWarning("");
    setConfirmDelete(false);
  }, [account]);

  const patch = useAccountsMutation((body: Record<string, unknown>) => api.patch(`/accounts/${account!.id}`, body));
  const members = useQuery(householdQuery).data?.members ?? [];
  const merge = useAccountsMutation(() => api.post("/accounts/merge", { from: account!.id, into: Number(mergeInto) }), onClose);
  const replace = useAccountsMutation(
    () => api.post<{ warning?: string }>(`/accounts/${account!.id}/replace`, { with: Number(replaceWith) }),
    (r) => (r.warning ? setReplaceWarning(r.warning) : onClose()),
  );
  const del = useAccountsMutation(() => api.del(`/accounts/${account!.id}`), onClose);

  if (!account) return <Sheet open={false} onOpenChange={onClose} title="" children={null} />;
  const others = accounts.filter((a) => a.id !== account.id && a.status !== "review" && !a.offered && !a.replaced_by);
  // Replacements: any other account (including ones waiting on SimpleFIN), same last 4 digits first.
  const replacements = accounts
    .filter((a) => a.id !== account.id && !a.builtin && !a.replaced_by && a.status !== "closed")
    .sort((x, y) => Number(y.mask === account.mask && !!account.mask) - Number(x.mask === account.mask && !!account.mask) || Number(y.offered) - Number(x.offered));
  const dirty = name !== account.name || type !== account.type || (account.is_manual && balance !== ((account.is_liability ? -1 : 1) * account.balance_cents / 100).toFixed(2));

  return (
    <Sheet open onOpenChange={(o) => !o && onClose()} title={account.name}>
      <div className="flex flex-col gap-6">
        <div>
          <MoneyText cents={account.balance_cents} className="text-2xl font-semibold" />
          <div className="mt-1 flex flex-wrap items-center gap-2 text-[13px] text-muted">
            <span>{accountSubtitle(account)}</span>
            <StatusBadge account={account} />
            <BillBadges bill={account.bill} />
            {account.hidden && <Badge>Hidden</Badge>}
          </div>
          {!account.is_manual && <div className="mt-1 text-xs text-muted">Last synced {timeAgo(account.last_synced_at)}</div>}
          {account.provider_name && account.provider_name !== account.name && (
            <div className="mt-1 text-xs text-muted">Bank name: {account.provider_name}</div>
          )}
        </div>

        <AccountHistory account={account} />

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

        <Appearance account={account} />

        <div className="flex flex-col gap-3 border-t border-border pt-5">
          {members.length > 1 && (
            <Select
              label="Owner"
              value={String(account.owner_id ?? "")}
              onChange={(e) => patch.mutate({ owner_id: e.target.value ? Number(e.target.value) : null })}
              options={[{ value: "", label: "Shared" }, ...members.map((m) => ({ value: String(m.id), label: m.name }))]}
            />
          )}
          <Switch
            label="Include in net worth"
            checked={account.include_in_net_worth}
            onCheckedChange={(v) => patch.mutate({ include_in_net_worth: v })}
          />
          <Switch label="Hide from lists" checked={account.hidden} onCheckedChange={(v) => patch.mutate({ hidden: v })} />
          {!account.is_manual && (
            <Switch
              label="Flip the bank's balance sign"
              hint="For a bank that reports this balance backwards through SimpleFIN, e.g. an overdrawn checking account showing as positive."
              checked={account.invert_balance}
              onCheckedChange={(v) => patch.mutate({ invert_balance: v })}
            />
          )}
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

        {!account.builtin && replacements.length > 0 && (
          <div className="flex flex-col gap-2 border-t border-border pt-5" data-testid="replace-account">
            <Select
              label="Replace account"
              value={replaceWith}
              onChange={(e) => setReplaceWith(e.target.value)}
              options={[
                { value: "", label: "Replace with…" },
                ...replacements.map((o) => ({
                  value: String(o.id),
                  label: `${o.name} (${accountSubtitle(o)}${o.offered ? ", new on SimpleFIN" : o.status === "ignored" ? ", not in Viceroy" : ""})`,
                })),
              ]}
            />
            <p className="text-xs text-muted" title="Banks and aggregators sometimes start reporting an account under a new id (after a reconnect, or through a second login), so it shows up twice and the old one may stop updating. Replacing moves everything to the new one and keeps the old link hidden so it isn't offered again.">
              For sync duplicates: when the bank now reports this account as a new one. The new account takes over this one's transactions, balance
              history, rules, email filters, recurring items, name and settings, and this one goes away.
            </p>
            <FormError error={replace.error} />
            {replaceWarning && <p className="rounded-lg bg-accent-soft px-3 py-2 text-[13px] text-accent">{replaceWarning}</p>}
            <div>
              <Button
                size="sm"
                variant="secondary"
                disabled={!replaceWith}
                loading={replace.isPending}
                onClick={() =>
                  confirm(`Replace “${account.name}” with “${replacements.find((o) => String(o.id) === replaceWith)?.name}”? This can't be undone.`) &&
                  replace.mutate(undefined)
                }
              >
                Replace
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

const colorNotes: Record<Account["color_source"], string> = {
  ai: "Picked by AI from the bank's brand.",
  auto: "Picked automatically.",
  user: "Your color.",
  "": "Picking a color…",
};

/** Color and logo: the AI picks the bank's color when the account is added; both can be changed. */
function Appearance({ account }: { account: Account }) {
  const file = useRef<HTMLInputElement>(null);
  const [color, setColor] = useState(account.color || "#888888");
  // What's typed in the hex box; it becomes the color once it's a full #rrggbb.
  const [hex, setHex] = useState(color);
  useEffect(() => setColor(account.color || "#888888"), [account.color]);
  useEffect(() => setHex(color), [color]);
  const patch = useAccountsMutation((body: Record<string, unknown>) => api.patch(`/accounts/${account.id}`, body));
  const suggest = useAccountsMutation(
    () => api.post<{ color: string }>(`/accounts/${account.id}/color/suggest`),
    (r) => setColor(r.color),
  );
  const upload = useAccountsMutation(async (f: File) => api.put(`/accounts/${account.id}/logo`, { image: await shrinkImage(f, 160, "image/png") }));
  const removeLogo = useAccountsMutation(() => api.del(`/accounts/${account.id}/logo`));
  const changed = color.toLowerCase() !== account.color.toLowerCase();
  return (
    <div className="flex flex-col gap-3 border-t border-border pt-5" data-testid="account-appearance">
      <h3 className="text-[13px] font-semibold">Appearance</h3>
      <div className="flex items-center gap-3">
        <AccountAvatar account={{ ...account, color }} size={40} />
        <label className="flex items-center gap-2 text-[13px]">
          <input type="color" value={color} onChange={(e) => setColor(e.target.value)} className="h-8 w-10 cursor-pointer rounded border border-border bg-surface" aria-label="Account color" />
          <input
            value={hex}
            onChange={(e) => {
              const v = e.target.value.trim();
              setHex(v);
              const full = (v.startsWith("#") ? v : "#" + v).toLowerCase();
              if (/^#[0-9a-f]{6}$/.test(full)) setColor(full);
            }}
            onBlur={() => setHex(color)}
            maxLength={7}
            spellCheck={false}
            aria-label="Hex color"
            className="h-8 w-[5.5rem] rounded-lg border border-border bg-surface px-2 font-mono text-xs outline-none focus:border-accent"
          />
        </label>
        {changed && (
          <Button size="sm" loading={patch.isPending} onClick={() => patch.mutate({ color })}>
            Save color
          </Button>
        )}
      </div>
      <p className="text-xs text-muted">{changed ? "Not saved yet." : colorNotes[account.color_source]}</p>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="secondary" loading={suggest.isPending} onClick={() => suggest.mutate(undefined)}>
          <Sparkles size={14} /> Suggest bank color
        </Button>
        <Button size="sm" variant="secondary" loading={upload.isPending} onClick={() => file.current?.click()}>
          <Upload size={14} /> {account.logo_url ? "Replace logo" : "Upload logo"}
        </Button>
        {account.logo_url && (
          <Button size="sm" variant="ghost" loading={removeLogo.isPending} onClick={() => removeLogo.mutate(undefined)}>
            Remove logo
          </Button>
        )}
        <input
          ref={file}
          type="file"
          accept="image/png,image/jpeg,image/webp,image/gif"
          className="hidden"
          data-testid="logo-file"
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) upload.mutate(f);
            e.target.value = "";
          }}
        />
      </div>
      <FormError error={patch.error ?? suggest.error ?? upload.error ?? removeLogo.error} />
    </div>
  );
}
