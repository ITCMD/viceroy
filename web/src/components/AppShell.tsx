import { Link, Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useEffect } from "react";
import { DropdownMenu } from "radix-ui";
import { LogOut, MoreHorizontal } from "lucide-react";
import { api } from "@/lib/api";
import { useRefreshSession, useSession } from "@/lib/session";
import { NotificationBell } from "@/features/notifications/NotificationBell";
import { EmailNoticeHost } from "@/features/email/NoticeActions";
import { InstallBanner } from "@/features/pwa/InstallBanner";
import { settingsQuery } from "@/features/settings/settings";
import { AppLogo, applyLogo, cachedLogo } from "./AppLogo";
import { navItems } from "./nav";

const linkBase = "flex items-center gap-3 rounded-lg px-3 py-2 text-[14px] font-medium text-muted transition hover:bg-surface-2 hover:text-text";
const linkActive = "!bg-accent-soft !text-accent";

export function AppShell() {
  const { data } = useSession();
  const { data: settings } = useQuery(settingsQuery);
  const logo = settings?.logo ?? cachedLogo();
  useEffect(() => applyLogo(logo), [logo]);
  const refresh = useRefreshSession();
  const navigate = useNavigate();
  const logout = useMutation({
    mutationFn: () => api.post("/auth/logout"),
    onSuccess: async () => {
      await refresh();
      navigate({ to: "/login" });
    },
  });

  return (
    <div className="flex h-full">
      <aside className="hidden w-56 shrink-0 flex-col border-r border-border bg-sidebar md:flex">
        <div className="flex h-14 items-center gap-2 px-5">
          <AppLogo logo={logo} size={28} />
          <span className="text-[16px] font-semibold tracking-tight">Viceroy</span>
          <NotificationBell className="ml-auto" />
        </div>
        <nav className="flex flex-1 flex-col gap-0.5 px-3 py-2">
          {navItems.map(({ to, label, icon: Icon }) => (
            <Link key={to} to={to} className={linkBase} activeProps={{ className: linkActive }} activeOptions={{ exact: to === "/" }}>
              <Icon size={18} />
              {label}
            </Link>
          ))}
        </nav>
        <div className="border-t border-border p-3">
          <Link to={"/settings" as string} search={{ tab: "household" } as never} className="block rounded-lg px-3 pb-2 pt-1 text-[13px] hover:bg-surface-2" title="Your account and household">
            <div className="truncate font-medium">{data?.user?.name}</div>
            <div className="truncate text-xs text-muted">{data?.household?.name}</div>
          </Link>
          <button className={linkBase + " w-full"} onClick={() => logout.mutate()}>
            <LogOut size={18} />
            Sign out
          </button>
        </div>
      </aside>

      <main className="min-w-0 flex-1 overflow-y-auto pb-20 md:pb-0">
        <Outlet />
      </main>
      <EmailNoticeHost />
      <InstallBanner />

      <nav className="fixed inset-x-0 bottom-0 z-20 flex border-t border-border bg-sidebar pb-[env(safe-area-inset-bottom)] md:hidden">
        {navItems
          .filter((n) => n.mobile)
          .map(({ to, label, icon: Icon }) => (
            <Link
              key={to}
              to={to}
              className="flex flex-1 flex-col items-center gap-0.5 py-2 text-[11px] font-medium text-muted"
              activeProps={{ className: "!text-accent" }}
              activeOptions={{ exact: to === "/" }}
            >
              <Icon size={20} />
              {label}
            </Link>
          ))}
        <MoreTab onSignOut={() => logout.mutate()} />
      </nav>
    </div>
  );
}

const moreItems = navItems.filter((n) => !n.mobile);

/** Phone tab bar overflow: every page that isn't a tab, plus sign out. */
function MoreTab({ onSignOut }: { onSignOut: () => void }) {
  const path = useLocation({ select: (l) => l.pathname });
  const navigate = useNavigate();
  const active = moreItems.some((n) => path === n.to || path.startsWith(n.to + "/"));
  const item = "flex cursor-pointer items-center gap-3 rounded-md px-3 py-2.5 text-[14px] outline-none data-[highlighted]:bg-surface-2";
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger className={"flex flex-1 flex-col items-center gap-0.5 py-2 text-[11px] font-medium outline-none " + (active ? "text-accent" : "text-muted")}>
        <MoreHorizontal size={20} />
        More
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content side="top" align="end" sideOffset={6} collisionPadding={8} className="z-[60] min-w-52 rounded-lg border border-border bg-surface p-1 shadow-xl">
          {moreItems.map(({ to, label, icon: Icon }) => {
            const on = path === to || path.startsWith(to + "/");
            return (
              <DropdownMenu.Item key={to} onSelect={() => navigate({ to })} className={item + (on ? " text-accent" : "")}>
                <Icon size={18} className={on ? "text-accent" : "text-muted"} />
                {label}
              </DropdownMenu.Item>
            );
          })}
          <DropdownMenu.Separator className="my-1 h-px bg-border" />
          <DropdownMenu.Item onSelect={onSignOut} className={item}>
            <LogOut size={18} className="text-muted" />
            Sign out
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}
