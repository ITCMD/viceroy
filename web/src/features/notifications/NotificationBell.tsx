import { useQuery } from "@tanstack/react-query";
import { useNavigate, useRouter } from "@tanstack/react-router";
import clsx from "clsx";
import { Bell, BellRing, CalendarClock, Gauge, Landmark, Receipt, TriangleAlert, Unplug, type LucideIcon } from "lucide-react";
import { Popover } from "radix-ui";
import { useState } from "react";
import { Segmented } from "@/components/ui";
import { timeAgo } from "@/lib/format";
import { notificationsQuery, useMarkRead, type NotificationItem } from "./api";

const kindIcon: Record<NotificationItem["kind"], { icon: LucideIcon; tone: string }> = {
  over_budget: { icon: TriangleAlert, tone: "bg-negative/10 text-negative" },
  pacing: { icon: Gauge, tone: "bg-accent-soft text-accent" },
  large_txn: { icon: Receipt, tone: "bg-accent-soft text-accent" },
  disconnected: { icon: Unplug, tone: "bg-negative/10 text-negative" },
  payment_due: { icon: CalendarClock, tone: "bg-negative/10 text-negative" },
  bank_notice: { icon: Landmark, tone: "bg-accent-soft text-accent" },
  test: { icon: BellRing, tone: "bg-surface-2 text-muted" },
};

/** Bell with an unread count; opening it lists recent alerts and marks them read. */
export function NotificationBell({ className }: { className?: string }) {
  const { data } = useQuery(notificationsQuery);
  const markRead = useMarkRead();
  const navigate = useNavigate();
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState<"alerts" | "bank">("alerts");
  const unread = data?.unread ?? 0;
  const all = data?.notifications ?? [];
  // Messages the AI read from bank emails get their own tab, apart from Viceroy's own alerts.
  const bank = all.filter((n) => n.kind === "bank_notice");
  const items = tab === "bank" ? bank : all.filter((n) => n.kind !== "bank_notice");
  const unreadIn = (list: NotificationItem[]) => list.filter((n) => !n.read).length;

  const onOpenChange = (v: boolean) => {
    setOpen(v);
    if (!v && unread > 0) markRead.mutate();
  };

  return (
    <Popover.Root open={open} onOpenChange={onOpenChange}>
      <Popover.Trigger
        className={clsx("relative grid size-8 place-items-center rounded-lg text-muted transition hover:bg-surface-2 hover:text-text", className)}
        aria-label={unread ? `Notifications (${unread} unread)` : "Notifications"}
      >
        <Bell size={18} />
        {unread > 0 && (
          <span className="absolute right-0.5 top-0.5 grid h-4 min-w-4 place-items-center rounded-full bg-accent px-1 text-[10px] font-semibold leading-none text-accent-fg">
            {unread > 9 ? "9+" : unread}
          </span>
        )}
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          align="end"
          sideOffset={6}
          collisionPadding={12}
          className="z-50 flex max-h-[70vh] w-[min(24rem,calc(100vw-1.5rem))] flex-col rounded-xl border border-border bg-surface shadow-xl outline-none"
        >
          <header className="flex items-center justify-between border-b border-border px-4 py-3">
            <h2 className="text-[15px] font-semibold">Notifications</h2>
            <button
              className="text-[13px] font-medium text-accent hover:underline"
              onClick={() => {
                setOpen(false);
                navigate({ to: "/settings" as string, hash: "notifications" });
              }}
            >
              Settings
            </button>
          </header>
          {bank.length > 0 && (
            <div className="border-b border-border px-4 py-2">
              <Segmented
                label="Notification type"
                value={tab}
                onChange={setTab}
                items={[
                  { value: "alerts", label: `Alerts${unreadIn(all) - unreadIn(bank) ? ` (${unreadIn(all) - unreadIn(bank)})` : ""}` },
                  { value: "bank", label: `From your bank${unreadIn(bank) ? ` (${unreadIn(bank)})` : ""}` },
                ]}
              />
            </div>
          )}
          {items.length === 0 ? (
            <p className="px-4 py-10 text-center text-sm text-muted">
              {tab === "bank"
                ? "Nothing from your bank yet."
                : "No alerts yet. Over-budget, pacing, large transaction, payment and sync alerts show up here."}
            </p>
          ) : (
            <ul className="divide-y divide-border overflow-y-auto" data-testid="notification-list">
              {items.map((n) => {
                const { icon: Icon, tone } = kindIcon[n.kind] ?? kindIcon.test;
                return (
                  <li key={n.id}>
                    <button
                      className="flex w-full gap-3 px-4 py-3 text-left hover:bg-surface-2"
                      onClick={() => {
                        onOpenChange(false);
                        // URLs may carry a query, e.g. a bank notice's "/accounts?email=12".
                        if (n.url) router.history.push(n.url);
                      }}
                    >
                      <span className={clsx("grid size-8 shrink-0 place-items-center rounded-full", tone)}>
                        <Icon size={15} />
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className={clsx("block text-sm", !n.read && "font-semibold")}>{n.title}</span>
                        {n.body && <span className="mt-0.5 block text-[13px] text-muted">{n.body}</span>}
                        <span className="mt-0.5 block text-xs text-muted">{timeAgo(n.created_at)}</span>
                      </span>
                      {!n.read && <span className="mt-1.5 size-2 shrink-0 rounded-full bg-accent" aria-label="Unread" />}
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
