import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { AlertTriangle, ChevronRight, Landmark, Plus, RefreshCw } from "lucide-react";
import { useMemo, useState } from "react";
import { AreaChart } from "@/components/charts/AreaChart";
import { Badge, Button, Card, EmptyState, FormError, MoneyText, PageHeader, Tabs } from "@/components/ui";
import { api } from "@/lib/api";
import { timeAgo } from "@/lib/format";
import { AccountSheet } from "./AccountSheet";
import { AddAccountDialog } from "./AddAccountDialog";
import { ReviewDialog } from "./ReviewDialog";
import { StatusBadge } from "./StatusBadge";
import {
  accountsQuery,
  accountSubtitle,
  connectionsQuery,
  groupLabels,
  netWorthQuery,
  useAccountsMutation,
  type Account,
  type AccountGroup,
  type Connection,
} from "./api";

type Tab = "networth" | Exclude<AccountGroup, "other">;
const tabs: { value: Tab; label: string }[] = [
  { value: "networth", label: "Net worth" },
  { value: "cash", label: "Cash" },
  { value: "credit", label: "Credit cards" },
  { value: "investments", label: "Investments" },
  { value: "loans", label: "Loans" },
];
const groupOrder: AccountGroup[] = ["cash", "credit", "investments", "loans", "other"];
const ranges = [
  { days: 30, label: "1M" },
  { days: 90, label: "3M" },
  { days: 365, label: "1Y" },
  { days: 1825, label: "5Y" },
];

export function AccountsPage() {
  const [tab, setTab] = useState<Tab>("networth");
  const [adding, setAdding] = useState(false);
  const [reviewing, setReviewing] = useState(false);
  const [selected, setSelected] = useState<number | null>(null);
  const [showHidden, setShowHidden] = useState(false);
  const { data } = useQuery(accountsQuery);
  const { data: connData } = useQuery(connectionsQuery);
  const accounts = data?.accounts ?? [];
  const connections = connData?.connections ?? [];

  const syncAll = useAccountsMutation(() => Promise.all(connections.map((c) => api.post(`/connections/${c.id}/sync`))));

  const reviewCount = accounts.filter((a) => a.status === "review").length;
  const visible = accounts.filter((a) => a.status !== "review" && (showHidden || (!a.hidden && a.status !== "ignored")));
  const hiddenCount = accounts.filter((a) => a.status !== "review" && (a.hidden || a.status === "ignored")).length;
  const shown = tab === "networth" ? visible : visible.filter((a) => a.group === tab);
  const selectedAccount = accounts.find((a) => a.id === selected) ?? null;

  return (
    <>
      <PageHeader
        title="Accounts"
        actions={
          <>
            {connections.length > 0 && (
              <Button variant="secondary" size="sm" aria-label="Refresh" loading={syncAll.isPending} onClick={() => syncAll.mutate(undefined)}>
                {!syncAll.isPending && <RefreshCw size={14} />}
                <span className="hidden sm:inline">Refresh</span>
              </Button>
            )}
            <Button size="sm" onClick={() => setAdding(true)}>
              <Plus size={15} />
              Add account
            </Button>
          </>
        }
      />
      <div className="mx-auto flex max-w-6xl flex-col gap-4 p-4 md:p-6">
        <FormError error={syncAll.error} />
        {reviewCount > 0 && (
          <button
            onClick={() => setReviewing(true)}
            className="flex items-center gap-3 rounded-xl border border-accent/40 bg-accent-soft px-4 py-3 text-left text-sm text-accent"
          >
            <AlertTriangle size={16} />
            <span className="flex-1 font-medium">
              {reviewCount} {reviewCount === 1 ? "account needs" : "accounts need"} review after your last sync
            </span>
            <ChevronRight size={16} />
          </button>
        )}

        <Tabs value={tab} onChange={setTab} items={tabs} />

        {accounts.length === 0 && data ? (
          <Card>
            <EmptyState icon={Landmark} title="No accounts yet">
              Connect your banks with SimpleFIN or add a manual account to start tracking your net worth.
              <div className="mt-4">
                <Button size="sm" onClick={() => setAdding(true)}>
                  <Plus size={15} />
                  Add account
                </Button>
              </div>
            </EmptyState>
          </Card>
        ) : (
          <div className="grid gap-4 lg:grid-cols-[1fr_300px]">
            <div className="flex min-w-0 flex-col gap-4">
              {tab === "networth" ? <NetWorthCard /> : <GroupSummary accounts={shown} label={groupLabels[tab]} />}
              {groupOrder
                .filter((g) => tab === "networth" || g === tab)
                .map((g) => {
                  const list = shown.filter((a) => a.group === g);
                  return list.length > 0 && <AccountGroupCard key={g} label={groupLabels[g]} accounts={list} onSelect={setSelected} />;
                })}
              {shown.length === 0 && (
                <Card>
                  <p className="py-6 text-center text-sm text-muted">No {groupLabels[tab as AccountGroup]?.toLowerCase()} accounts.</p>
                </Card>
              )}
              {hiddenCount > 0 && (
                <button className="self-start text-[13px] text-muted hover:text-text" onClick={() => setShowHidden(!showHidden)}>
                  {showHidden ? "Hide" : "Show"} {hiddenCount} hidden or ignored
                </button>
              )}
            </div>
            <div className="flex flex-col gap-4">
              <Summary accounts={accounts} />
              {connections.map((c) => (
                <ConnectionCard key={c.id} connection={c} />
              ))}
            </div>
          </div>
        )}
      </div>

      <AddAccountDialog open={adding} onOpenChange={setAdding} />
      <ReviewDialog open={reviewing} onOpenChange={setReviewing} accounts={accounts} />
      <AccountSheet account={selectedAccount} accounts={accounts} onClose={() => setSelected(null)} />
    </>
  );
}

function NetWorthCard() {
  const [days, setDays] = useState(90);
  const { data } = useQuery(netWorthQuery(days));
  const points = useMemo(() => (data?.points ?? []).map((p) => ({ date: p.date, value: p.net })), [data]);
  const last = points.at(-1)?.value ?? 0;
  const first = points[0]?.value ?? 0;
  const change = last - first;
  const label = ranges.find((r) => r.days === days)!.label;

  return (
    <Card>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="text-[13px] font-medium text-muted">Net worth</div>
          <MoneyText cents={last} className="text-3xl font-semibold tracking-tight" />
          <div className={clsx("mt-0.5 text-[13px] tabular", change > 0 ? "text-positive" : change < 0 ? "text-negative" : "text-muted")}>
            {change >= 0 ? "+" : "−"}
            <MoneyText cents={Math.abs(change)} /> <span className="text-muted">over {label}</span>
          </div>
        </div>
        <div className="flex rounded-lg border border-border p-0.5" role="group" aria-label="Time range">
          {ranges.map((r) => (
            <button
              key={r.days}
              onClick={() => setDays(r.days)}
              aria-pressed={r.days === days}
              className={clsx("rounded-md px-2.5 py-1 text-xs font-medium", r.days === days ? "bg-surface-2 text-text" : "text-muted hover:text-text")}
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>
      <div className="mt-3">
        <AreaChart points={points} label="Net worth" />
      </div>
    </Card>
  );
}

function GroupSummary({ accounts, label }: { accounts: Account[]; label: string }) {
  const total = accounts.filter((a) => a.include_in_net_worth).reduce((s, a) => s + a.balance_cents, 0);
  return (
    <Card>
      <div className="text-[13px] font-medium text-muted">{label}</div>
      <MoneyText cents={total} className="text-3xl font-semibold tracking-tight" />
      <div className="mt-0.5 text-[13px] text-muted">
        {accounts.length} {accounts.length === 1 ? "account" : "accounts"}
      </div>
    </Card>
  );
}

function AccountGroupCard({ label, accounts, onSelect }: { label: string; accounts: Account[]; onSelect: (id: number) => void }) {
  const total = accounts.filter((a) => a.include_in_net_worth && a.status !== "ignored").reduce((s, a) => s + a.balance_cents, 0);
  return (
    <section className="rounded-xl border border-border bg-surface">
      <header className="flex items-center justify-between border-b border-border px-4 py-3">
        <h2 className="text-[15px] font-semibold">{label}</h2>
        <MoneyText cents={total} className="text-[15px] font-semibold" />
      </header>
      <ul className="divide-y divide-border">
        {accounts.map((a) => (
          <li key={a.id}>
            <button onClick={() => onSelect(a.id)} className="flex w-full items-center gap-3 px-4 py-3 text-left transition hover:bg-surface-2" data-testid="account-row">
              <span className="grid size-8 shrink-0 place-items-center rounded-full bg-surface-2 text-xs font-semibold text-muted">
                {(a.institution_name || a.name).slice(0, 1).toUpperCase()}
              </span>
              <span className="min-w-0 flex-1">
                <span className="flex items-center gap-2">
                  <span className="truncate text-sm font-medium">{a.name}</span>
                  <StatusBadge account={a} />
                </span>
                <span className="block truncate text-xs text-muted">{accountSubtitle(a)}</span>
              </span>
              <span className="text-right">
                <MoneyText cents={a.balance_cents} className={clsx("block text-sm font-medium", !a.include_in_net_worth && "text-muted")} />
                <span className="block text-xs text-muted">{a.is_manual ? "Manual" : a.status === "disconnected" ? "Not syncing" : timeAgo(a.last_synced_at)}</span>
              </span>
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}

function Summary({ accounts }: { accounts: Account[] }) {
  const counted = accounts.filter((a) => a.include_in_net_worth && a.status !== "ignored" && a.status !== "review");
  const assets = counted.filter((a) => a.balance_cents >= 0).reduce((s, a) => s + a.balance_cents, 0);
  const liabilities = counted.filter((a) => a.balance_cents < 0).reduce((s, a) => s - a.balance_cents, 0);
  const total = assets + liabilities || 1;
  return (
    <Card title="Summary">
      <div className="flex flex-col gap-3 text-sm">
        <div className="flex h-2 gap-0.5 overflow-hidden rounded-full" aria-hidden>
          <div className="rounded-full bg-positive" style={{ width: `${(assets / total) * 100}%` }} />
          <div className="rounded-full bg-negative" style={{ width: `${(liabilities / total) * 100}%` }} />
        </div>
        <Row label="Assets" dot="bg-positive" cents={assets} />
        <Row label="Liabilities" dot="bg-negative" cents={liabilities} />
      </div>
    </Card>
  );
}

function Row({ label, dot, cents }: { label: string; dot: string; cents: number }) {
  return (
    <div className="flex items-center justify-between">
      <span className="flex items-center gap-2 text-muted">
        <span className={clsx("size-2 rounded-full", dot)} />
        {label}
      </span>
      <MoneyText cents={cents} className="font-medium" />
    </div>
  );
}

function ConnectionCard({ connection: c }: { connection: Connection }) {
  const [showLog, setShowLog] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const sync = useAccountsMutation(() => api.post(`/connections/${c.id}/sync`));
  const remove = useAccountsMutation(() => api.del(`/connections/${c.id}`));
  // A bank re-linked on the Bridge gets a new conn_id; show each bank once, worst status first.
  const banks = [...new Map([...c.institutions].sort((a, b) => Number(a.status === "reauth") - Number(b.status === "reauth")).map((i) => [i.name, i])).values()];
  const reauth = banks.filter((i) => i.status === "reauth");

  return (
    <Card title={c.name} action={c.status !== "active" ? <Badge tone="negative">{c.status === "revoked" ? "Access revoked" : "Error"}</Badge> : undefined}>
      <div className="flex flex-col gap-3 text-[13px]">
        <div className="text-muted">
          Synced {timeAgo(c.last_sync_at)} · {c.requests_remaining} syncs left today
        </div>
        {c.last_error && <p className="rounded-lg bg-negative/10 px-3 py-2 text-negative">{c.last_error}</p>}
        {reauth.map((i) => (
          <p key={i.id} className="rounded-lg bg-negative/10 px-3 py-2 text-negative">
            {i.name}: {i.last_error || "sign in again"}.{" "}
            <a className="underline" href="https://bridge.simplefin.org/my-account" target="_blank" rel="noreferrer">
              Fix on SimpleFIN Bridge
            </a>
          </p>
        ))}
        {banks.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {banks.map((i) => (
              <Badge key={i.id} tone={i.status === "reauth" ? "negative" : "neutral"}>
                {i.name}
              </Badge>
            ))}
          </div>
        )}
        <FormError error={sync.error ?? remove.error} />
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="secondary" loading={sync.isPending} disabled={c.requests_remaining === 0} onClick={() => sync.mutate(undefined)}>
            Sync now
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setShowLog(!showLog)}>
            {showLog ? "Hide" : "History"}
          </Button>
          {confirmRemove ? (
            <Button size="sm" variant="danger" loading={remove.isPending} onClick={() => remove.mutate(undefined)}>
              Confirm remove
            </Button>
          ) : (
            <Button size="sm" variant="danger-ghost" onClick={() => setConfirmRemove(true)}>
              Remove
            </Button>
          )}
        </div>
        {confirmRemove && <p className="text-xs text-muted">Accounts stay with their history but stop syncing.</p>}
        {showLog && (
          <ul className="flex flex-col gap-1.5 border-t border-border pt-3">
            {c.events.map((e, i) => (
              <li key={i} className="flex gap-2">
                <span className="w-14 shrink-0 text-muted">{timeAgo(e.at)}</span>
                <span className={clsx(e.kind === "sync_error" && "text-negative")}>{e.message}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Card>
  );
}
