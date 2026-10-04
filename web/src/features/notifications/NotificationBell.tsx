import { useQuery } from "@tanstack/react-query";
import { useNavigate, useRouter, useRouterState } from "@tanstack/react-router";
import clsx from "clsx";
import {
  Bell,
  BellRing,
  CalendarCheck,
  CalendarClock,
  CircleCheck,
  FileText,
  Filter,
  Gauge,
  Landmark,
  Receipt,
  Scale,
  ShieldAlert,
  TriangleAlert,
  Unplug,
  type LucideIcon,
} from "lucide-react";
import { Popover } from "radix-ui";
import { useState } from "react";
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

// Bank notices share one kind; their titles start with what the email was about
// (see noticeTitle and learnFilter in internal/email/airead.go).
const noticeIcon: [string, { icon: LucideIcon; tone: string }][] = [
  ["Payment due", { icon: CalendarClock, tone: "bg-negative/10 text-negative" }],
  ["Payment scheduled", { icon: CalendarCheck, tone: "bg-accent-soft text-accent" }],
  ["Payment received", { icon: CircleCheck, tone: "bg-positive/10 text-positive" }],
  ["Security alert", { icon: ShieldAlert, tone: "bg-negative/10 text-negative" }],
  ["Statement ready", { icon: FileText, tone: "bg-surface-2 text-muted" }],
  ["Balance", { icon: Scale, tone: "bg-accent-soft text-accent" }],
  ["New email filter", { icon: Filter, tone: "bg-accent-soft text-accent" }],
];

function iconFor(n: NotificationItem) {
  if (n.kind === "bank_notice") return noticeIcon.find(([p]) => n.title.startsWith(p))?.[1] ?? kindIcon.bank_notice;
  return kindIcon[n.kind] ?? kindIcon.test;
}

/** Bell with an unread count; opening it lists recent alerts and marks them read. */
export function NotificationBell({ className }: { className?: string }) {
  const { data } = useQuery(notificationsQuery);
  const markRead = useMarkRead();
  const navigate = useNavigate();
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const location = useRouterState({ select: (s) => s.location });
  const unread = data?.unread ?? 0;
  const items = data?.notifications ?? [];

  const go = (url: string) => {
    if (!url) return;
    const u = new URL(url, window.location.origin);
    // A bank notice opens its email in a sheet over whatever page is showing, so it
    // doesn't pull you away to Accounts (push notifications still land there).
    const email = u.searchParams.get("email");
    if (email) {
      navigate({ to: location.pathname as string, search: { ...(location.search as object), email: Number(email) } as never });
    } else {
      router.history.push(url); // URLs may carry a query or a hash
    }
  };

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
                navigate({ to: "/settings" as string, search: { tab: "notifications" } as never });
              }}
            >
              Settings
            </button>
          </header>
          {items.length === 0 ? (
            <p className="px-4 py-10 text-center text-sm text-muted">
              No alerts yet. Over-budget, pacing, large transaction, payment and sync alerts show up here, along with anything your bank emails about.
            </p>
          ) : (
            <ul className="divide-y divide-border overflow-y-auto" data-testid="notification-list">
              {items.map((n) => {
                const { icon: Icon, tone } = iconFor(n);
                return (
                  <li key={n.id}>
                    <button
                      className="flex w-full gap-3 px-4 py-3 text-left hover:bg-surface-2"
                      onClick={() => {
                        onOpenChange(false);
                        go(n.url);
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
