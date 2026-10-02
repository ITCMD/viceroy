import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { householdQuery, ownerName } from "@/features/household/api";
import clsx from "clsx";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { ArrowLeftRight, ChevronDown, EyeOff, Link2, Mail, Plus, Search, SlidersHorizontal, Sparkles } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Badge, Button, Card, CategoryIcon, EmptyState, Menu, MoneyText, PageHeader, Segmented } from "@/components/ui";
import { accountLabel, accountsQuery, canAddTo } from "@/features/accounts/api";
import { AddTransactionDialog } from "./AddTransactionDialog";
import { AICategorizeDialog } from "./AICategorizeDialog";
import { TransactionSheet } from "./TransactionSheet";
import { dayLabel, pendingLabel, readTxnLink, transactionsQuery, txnSearch, type Transaction, type TxnFilters, type TxnLink } from "./api";
import { filterCount, TxnFilterChips, TxnFilterPanel } from "./TxnFilters";
import { EmailReviewBanner } from "@/features/email/EmailReviewBanner";

const views: { value: NonNullable<TxnFilters["view"]>; label: string }[] = [
  { value: "all", label: "All" },
  { value: "review", label: "Needs review" },
  { value: "uncategorized", label: "Uncategorized" },
];

export function TransactionsPage() {
  const [search, setSearch] = useState("");
  const [q, setQ] = useState("");
  const [account, setAccount] = useState(0);
  const [owner, setOwner] = useState(-1); // -1 = everyone, 0 = shared
  const members = useQuery(householdQuery).data?.members ?? [];
  const [view, setView] = useState<NonNullable<TxnFilters["view"]>>("all");
  const [hidden, setHidden] = useState(false);
  const [selected, setSelected] = useState<number | null>(null);
  const [adding, setAdding] = useState(false);
  const [categorizing, setCategorizing] = useState(false);

  useEffect(() => {
    const t = setTimeout(() => setQ(search.trim()), 250);
    return () => clearTimeout(t);
  }, [search]);

  const { data: acctData } = useQuery(accountsQuery);
  const accounts = (acctData?.accounts ?? []).filter((a) => a.status !== "ignored" && !(a.builtin && a.hidden && a.status === "closed"));
  const addable = accounts.filter(canAddTo);
  const routerSearch = useRouterState({ select: (s) => s.location.search as Record<string, unknown> });
  const link = useMemo(() => readTxnLink(routerSearch), [routerSearch]);
  const navigate = useNavigate();
  const setLink = useCallback(
    (next: TxnLink) => navigate({ to: "/transactions" as string, search: txnSearch(next) as never, replace: true }),
    [navigate],
  );
  const [showFilters, setShowFilters] = useState(false);
  const filters: TxnFilters = { ...link, account: account || undefined, owner: owner >= 0 ? owner : undefined, q: q || undefined, view, hidden };
  const list = useInfiniteQuery(transactionsQuery(filters));
  const txns = useMemo(() => list.data?.pages.flatMap((p) => p.transactions) ?? [], [list.data]);
  const days = useMemo(() => {
    const out: { date: string; txns: Transaction[] }[] = [];
    for (const t of txns) {
      if (out.at(-1)?.date !== t.date) out.push({ date: t.date, txns: [] });
      out.at(-1)!.txns.push(t);
    }
    return out;
  }, [txns]);
  const linked = Object.keys(link).length > 0;
  const filtered = !!(q || account || owner >= 0 || view !== "all" || linked);

  return (
    <>
      <PageHeader
        title="Transactions"
        actions={
          <>
            <Menu label="More transaction actions" items={[{ label: "Categorize with AI…", icon: Sparkles, onSelect: () => setCategorizing(true) }]} />
            <Button size="sm" onClick={() => setAdding(true)} disabled={addable.length === 0}>
              <Plus size={15} />
              <span className="hidden sm:inline">Add transaction</span>
              <span className="sm:hidden">Add</span>
            </Button>
          </>
        }
      />
      <div className="mx-auto flex max-w-4xl flex-col gap-4 p-4 md:p-6">
        <EmailReviewBanner />
        <div className="flex flex-wrap items-center gap-2">
          <label className="relative min-w-48 flex-1">
            <span className="sr-only">Search transactions</span>
            <Search size={14} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted" />
            <input
              type="search"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search merchants, statements, notes"
              className="h-9 w-full rounded-lg border border-border bg-surface pl-8 pr-3 text-sm outline-none transition placeholder:text-muted focus:border-accent focus:ring-2 focus:ring-accent/20"
            />
          </label>
          <label className="relative">
            <span className="sr-only">Account</span>
            <select
              value={account}
              onChange={(e) => setAccount(Number(e.target.value))}
              className="h-9 max-w-52 appearance-none rounded-lg border border-border bg-surface pl-3 pr-8 text-sm outline-none focus:border-accent focus:ring-2 focus:ring-accent/20"
            >
              <option value={0}>All accounts</option>
              {accounts.map((a) => (
                <option key={a.id} value={a.id}>
                  {accountLabel(a)}
                </option>
              ))}
            </select>
            <ChevronDown size={14} className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-muted" />
          </label>
          <Button
            variant="secondary"
            size="sm"
            className="h-9"
            onClick={() => setShowFilters((v) => !v)}
            aria-expanded={showFilters}
            data-testid="txn-filters-toggle"
          >
            <SlidersHorizontal size={14} />
            Filters
            {linked && <span className="grid size-4 place-items-center rounded-full bg-accent text-[10px] font-semibold text-accent-fg">{filterCount(link)}</span>}
          </Button>
          {members.length > 1 && (
            <label className="relative">
              <span className="sr-only">Owner</span>
              <select
                value={owner}
                onChange={(e) => setOwner(Number(e.target.value))}
                className="h-9 max-w-40 appearance-none rounded-lg border border-border bg-surface pl-3 pr-8 text-sm outline-none focus:border-accent focus:ring-2 focus:ring-accent/20"
              >
                <option value={-1}>Everyone</option>
                {members.map((m) => (
                  <option key={m.id} value={m.id}>
                    {ownerName(members, m.id)}
                  </option>
                ))}
                <option value={0}>Shared</option>
              </select>
              <ChevronDown size={14} className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-muted" />
            </label>
          )}
        </div>
        {showFilters && <TxnFilterPanel link={link} onChange={setLink} />}
        <TxnFilterChips link={link} onChange={setLink} />
        <div className="flex flex-wrap items-center justify-between gap-2">
          <Segmented label="Show" value={view} onChange={setView} items={views} />
          <label className="flex items-center gap-2 text-[13px] text-muted">
            <input type="checkbox" checked={hidden} onChange={(e) => setHidden(e.target.checked)} className="accent-accent" />
            Show hidden
          </label>
        </div>

        {list.data && txns.length === 0 ? (
          <Card>
            <EmptyState icon={ArrowLeftRight} title={filtered ? "No matching transactions" : "No transactions yet"}>
              {filtered
                ? "Try a different search or filter."
                : accounts.length
                  ? "Transactions appear here after your accounts sync. You can also add one by hand."
                  : "Add an account first, then its transactions show up here."}
            </EmptyState>
          </Card>
        ) : (
          days.length > 0 && (
            <div className="overflow-hidden rounded-xl border border-border bg-surface">
              {days.map((d) => (
                <DayGroup key={d.date} date={d.date} txns={d.txns} onSelect={setSelected} />
              ))}
            </div>
          )
        )}
        {list.hasNextPage && (
          <Button variant="secondary" size="sm" className="self-center" loading={list.isFetchingNextPage} onClick={() => list.fetchNextPage()}>
            Load more
          </Button>
        )}
      </div>

      <AICategorizeDialog open={categorizing} onOpenChange={setCategorizing} />
      <AddTransactionDialog
        open={adding}
        onOpenChange={setAdding}
        accounts={addable}
        defaultAccount={addable.some((a) => a.id === account) ? account : undefined} onCreated={setSelected} />
      <TransactionSheet id={selected} onClose={() => setSelected(null)} onSelect={setSelected} />
    </>
  );
}

function DayGroup({ date, txns, onSelect }: { date: string; txns: Transaction[]; onSelect: (id: number) => void }) {
  const total = txns.filter((t) => !t.hidden).reduce((s, t) => s + t.amount_cents, 0);
  return (
    <section className="border-b border-border last:border-b-0" data-testid="txn-day">
      <header className="flex items-center justify-between border-b border-border bg-surface-2 px-4 py-1.5">
        <h2 className="text-[13px] font-semibold">{dayLabel(date)}</h2>
        <MoneyText cents={total} className="text-[13px] text-muted" />
      </header>
      <ul className="divide-y divide-border">
        {txns.map((t) => (
          <li key={t.id}>
            <TransactionRow txn={t} onClick={() => onSelect(t.id)} />
          </li>
        ))}
      </ul>
    </section>
  );
}

export function TransactionRow({ txn: t, onClick }: { txn: Transaction; onClick: () => void }) {
  return (
    <button onClick={onClick} className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition hover:bg-surface-2" data-testid="txn-row">
      <CategoryIcon icon={t.category_icon} />
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-1.5">
          <span className={clsx("truncate text-sm font-medium", t.hidden && "text-muted")}>{t.merchant}</span>
          {t.needs_review && <span className="size-1.5 shrink-0 rounded-full bg-accent" title="Needs review" aria-label="Needs review" />}
          {t.source === "email" && <Mail size={13} className="shrink-0 text-muted" aria-label="From an email alert" />}
          {t.has_linked && t.linked_source !== "email" && <Link2 size={13} className="shrink-0 text-muted" aria-label="Linked to a pending entry" />}
          {t.hidden && <EyeOff size={13} className="shrink-0 text-muted" aria-label="Hidden" />}
        </span>
        <span className="block truncate text-xs text-muted">
          {t.category_name || "Uncategorized"} · {t.account_name}
        </span>
      </span>
      <span className="flex shrink-0 flex-col items-end gap-0.5">
        <MoneyText cents={t.amount_cents} colored className={clsx("text-sm font-medium", t.hidden && "text-muted line-through")} />
        {t.pending && <Badge>{pendingLabel(t)}</Badge>}
      </span>
    </button>
  );
}
