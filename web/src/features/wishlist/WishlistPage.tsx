import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import clsx from "clsx";
import { ChevronDown, ExternalLink, Gift, Pencil, Plus, ShoppingBag, Trash2, Undo2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Badge, Button, Card, EmptyState, Menu, MoneyText, PageHeader, Segmented } from "@/components/ui";
import { formatMoney } from "@/lib/format";
import { useSession } from "@/lib/session";
import { BoughtDialog } from "./BoughtDialog";
import { StarPicker } from "./StarPicker";
import { WishItemDialog } from "./WishItemDialog";
import { useWishMutation, wishlistQuery, type Afford, type Member, type WishItem, type WishSort } from "./api";
import { api } from "@/lib/api";

const sorts: { value: WishSort; label: string }[] = [
  { value: "added", label: "Newest" },
  { value: "price", label: "Price" },
  { value: "score", label: "Best value" },
];

const SORT_KEY = "viceroy.wishlist.sort";

function loadSort(): WishSort {
  try {
    const v = localStorage.getItem(SORT_KEY);
    return v === "price" || v === "score" ? v : "added";
  } catch {
    return "added";
  }
}

/** Looks like a pasted product link. */
const isLink = (s: string) => /^https?:\/\/\S+$/i.test(s.trim()) || /^(www\.)?[a-z0-9-]+(\.[a-z0-9-]+)+\/\S*$/i.test(s.trim());

export function WishlistPage() {
  const [sort, setSortState] = useState<WishSort>(loadSort);
  const [person, setPerson] = useState(0);
  const { data } = useQuery(wishlistQuery(sort, person));
  const { data: session } = useSession();
  const [editing, setEditing] = useState<{ item: WishItem | null; url?: string } | null>(null);
  const [buying, setBuying] = useState<WishItem | null>(null);
  const [showBought, setShowBought] = useState(false);
  const setSort = (s: WishSort) => {
    setSortState(s);
    try {
      localStorage.setItem(SORT_KEY, s);
    } catch {
      /* private mode */
    }
  };

  // Pasting a link anywhere on the page (outside a field) starts a new item with it.
  useEffect(() => {
    const onPaste = (e: ClipboardEvent) => {
      const el = e.target instanceof Element ? e.target : null;
      if (editing || buying || el?.closest("input, textarea, [contenteditable], [role=dialog]")) return;
      const text = e.clipboardData?.getData("text") ?? "";
      if (isLink(text)) {
        e.preventDefault();
        setEditing({ item: null, url: text.trim() });
      }
    };
    window.addEventListener("paste", onPaste);
    return () => window.removeEventListener("paste", onPaste);
  }, [editing, buying]);

  const members = data?.members ?? [];
  const items = data?.items ?? [];
  const bought = data?.bought ?? [];
  return (
    <>
      <PageHeader
        title="Wishlist"
        actions={
          <Button size="sm" onClick={() => setEditing({ item: null })}>
            <Plus size={15} /> Add item
          </Button>
        }
      />
      <div className="mx-auto flex max-w-6xl flex-col gap-4 p-4 md:p-6">
        {data && <AffordCard a={data.afford} items={items} />}
        <div className="flex flex-wrap items-center justify-between gap-2">
          <Segmented label="Sort" value={sort} onChange={setSort} items={sorts} />
          {members.length > 1 && (
            <Segmented
              label="Wanted by"
              value={person}
              onChange={setPerson}
              items={[{ value: 0, label: "Everyone" }, ...members.map((m) => ({ value: m.id, label: m.name.split(" ")[0] }))]}
            />
          )}
        </div>
        {data && items.length === 0 ? (
          <Card>
            <EmptyState icon={Gift} title={person ? "Nothing on their list" : "Your wishlist is empty"}>
              Add things you'd like to buy, or paste a product link anywhere on this page. Budget a monthly amount for the Wishlist goal and Viceroy shows what you can afford.
            </EmptyState>
          </Card>
        ) : (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3" data-testid="wishlist-grid">
            {items.map((it) => (
              <ItemCard key={it.id} it={it} sort={sort} members={members} onEdit={() => setEditing({ item: it })} onBuy={() => setBuying(it)} />
            ))}
          </div>
        )}
        {bought.length > 0 && (
          <section className="flex flex-col gap-2">
            <button type="button" onClick={() => setShowBought(!showBought)} className="flex items-center gap-1.5 self-start text-[13px] font-medium text-muted hover:text-text" aria-expanded={showBought}>
              <ChevronDown size={14} className={clsx("transition", !showBought && "-rotate-90")} />
              Bought ({bought.length})
            </button>
            {showBought && (
              <Card className="overflow-hidden [&>div]:p-0">
                <ul className="divide-y divide-border" data-testid="wishlist-bought">
                  {bought.map((it) => (
                    <BoughtRow key={it.id} it={it} />
                  ))}
                </ul>
              </Card>
            )}
          </section>
        )}
      </div>
      <WishItemDialog draft={editing} members={members} me={session?.user?.id ?? 0} onClose={() => setEditing(null)} />
      <BoughtDialog item={buying} onClose={() => setBuying(null)} />
    </>
  );
}

const fmtDay = (d: string) => new Date(d + "T00:00:00").toLocaleDateString("en-US", { month: "short", day: "numeric" });

function AffordCard({ a, items }: { a: Afford; items: WishItem[] }) {
  const now = items.filter((i) => i.afford === "now");
  const later = items.filter((i) => i.afford === "month_end");
  const left = Math.max(0, a.month_budget - a.month_contributed);
  return (
    <Card>
      <div className="grid gap-4 sm:grid-cols-[1fr_1fr_1.4fr]" data-testid="wishlist-afford">
        <div>
          <div className="text-[13px] text-muted">
            {a.goal_icon} {a.goal_name} saved
          </div>
          <MoneyText cents={a.saved_now} className="text-2xl font-semibold" />
        </div>
        <div>
          <div className="text-[13px] text-muted">By {fmtDay(a.month_end_date)}</div>
          <MoneyText cents={a.month_end} className="text-2xl font-semibold" />
          <div className="text-xs text-muted">
            {a.month_budget === 0
              ? "No monthly contribution yet"
              : !a.on_budget
                ? "Over budget this month, so this assumes nothing more is added."
                : left > 0
                  ? `+${formatMoney(left)} still to go in this month`
                  : `This month's ${formatMoney(a.month_budget, { whole: true })} is in`}
          </div>
        </div>
        <div className="flex flex-col justify-between gap-2 text-[13px]">
          <p>
            {now.length + later.length === 0 ? (
              <span className="text-muted">Nothing on the list fits yet.</span>
            ) : (
              <>
                {now.length > 0 && (
                  <span>
                    <strong>{now.length}</strong> {now.length === 1 ? "item" : "items"} affordable now
                  </span>
                )}
                {now.length > 0 && later.length > 0 && ", "}
                {later.length > 0 && (
                  <span>
                    <strong>{later.length}</strong> more by month end
                  </span>
                )}
                <span className="text-muted"> (in this order, top first)</span>
              </>
            )}
          </p>
          <Link to={"/budget" as string} className="font-medium text-accent hover:underline">
            {a.month_budget > 0 ? `Change the ${formatMoney(a.month_budget, { whole: true })}/month on the budget` : "Set a monthly amount on the budget"}
          </Link>
        </div>
      </div>
    </Card>
  );
}

function Initials({ m }: { m: Member }) {
  const ini = m.name
    .split(/\s+/)
    .map((w) => w[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();
  return (
    <span title={m.name} className="grid size-6 place-items-center rounded-full border-2 border-surface bg-accent-soft text-[10px] font-semibold text-accent">
      {ini}
    </span>
  );
}

function ItemCard({ it, sort, members, onEdit, onBuy }: { it: WishItem; sort: WishSort; members: Member[]; onEdit: () => void; onBuy: () => void }) {
  const stars = useWishMutation((n: number) => api.patch(`/wishlist/${it.id}`, { stars: n }));
  const del = useWishMutation(() => api.del(`/wishlist/${it.id}`));
  const wanters = members.filter((m) => it.wanted_by.includes(m.id));
  return (
    <article className="flex flex-col overflow-hidden rounded-xl border border-border bg-surface" data-testid="wish-card">
      <button type="button" onClick={onEdit} className="relative grid aspect-[16/10] place-items-center bg-white" aria-label={`Edit ${it.title}`}>
        {it.image_url ? (
          <img src={it.image_url} alt="" className="size-full object-contain p-3" />
        ) : (
          <span className="grid size-14 place-items-center rounded-full bg-accent-soft text-xl font-semibold text-accent">{it.title.charAt(0).toUpperCase()}</span>
        )}
        {it.afford && (
          <span className="absolute left-2 top-2">
            <Badge tone={it.afford === "now" ? "positive" : "accent"}>{it.afford === "now" ? "Affordable now" : "By month end"}</Badge>
          </span>
        )}
      </button>
      <div className="flex flex-1 flex-col gap-2 p-3">
        <div className="flex items-start gap-2">
          <button type="button" onClick={onEdit} className="line-clamp-2 min-w-0 flex-1 text-left text-sm font-medium hover:underline">
            {it.title}
          </button>
          <Menu
            label={`Actions for ${it.title}`}
            items={[
              { label: "Edit", icon: Pencil, onSelect: onEdit },
              ...(it.url ? [{ label: "Open link", icon: ExternalLink, onSelect: () => window.open(it.url, "_blank", "noopener,noreferrer") }] : []),
              { label: "Mark as bought", icon: ShoppingBag, onSelect: onBuy },
              { label: "Delete", icon: Trash2, onSelect: () => confirm(`Delete “${it.title}”?`) && del.mutate() },
            ]}
          />
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          {it.store && (
            <a href={it.url} target="_blank" rel="noopener noreferrer" className="rounded-full bg-surface-2 px-2 py-0.5 text-[11px] text-muted hover:text-text">
              {it.store}
            </a>
          )}
          {it.saves_money && <Badge tone="positive">Saves money</Badge>}
        </div>
        <div className="mt-auto flex items-end justify-between gap-2 pt-1">
          <div>
            {it.price_cents !== null ? <MoneyText cents={it.price_cents} className="text-base font-semibold" /> : <span className="text-[13px] text-muted">No price</span>}
            <StarPicker value={it.stars} onChange={(n) => stars.mutate(n)} size={14} label={`Stars for ${it.title}`} />
          </div>
          <div className="flex flex-col items-end gap-1">
            <div className="flex -space-x-1.5">
              {wanters.map((m) => (
                <Initials key={m.id} m={m} />
              ))}
            </div>
            {sort === "score" && it.score > 0 && (
              <span className="text-[11px] text-muted" title={it.score_text} data-testid="wish-score">
                {it.score_text}
              </span>
            )}
          </div>
        </div>
      </div>
    </article>
  );
}

function BoughtRow({ it }: { it: WishItem }) {
  const undo = useWishMutation(() => api.del(`/wishlist/${it.id}/bought`));
  return (
    <li className="flex items-center gap-3 px-4 py-2.5 text-[13px]">
      {it.image_url ? <img src={it.image_url} alt="" className="size-9 rounded bg-white object-contain" /> : <Gift size={18} className="text-muted" />}
      <div className="min-w-0 flex-1">
        <div className="truncate font-medium">{it.title}</div>
        <div className="text-xs text-muted">
          Bought {it.bought_at ? fmtDay(it.bought_at) : ""}
          {it.bought_txn_id ? ` · ${it.txn_name}` : ""}
        </div>
      </div>
      {it.bought_txn_id ? <MoneyText cents={it.txn_amount} /> : it.price_cents !== null && <MoneyText cents={it.price_cents} className="text-muted" />}
      <Button variant="ghost" size="sm" onClick={() => undo.mutate()} loading={undo.isPending} aria-label={`Put ${it.title} back on the list`}>
        <Undo2 size={14} />
      </Button>
    </li>
  );
}
