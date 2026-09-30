import { Link, Outlet, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useEffect } from "react";
import { LogOut } from "lucide-react";
import { api } from "@/lib/api";
import { useRefreshSession, useSession } from "@/lib/session";
import { NotificationBell } from "@/features/notifications/NotificationBell";
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
          <div className="px-3 pb-2 text-[13px]">
            <div className="truncate font-medium">{data?.user?.name}</div>
            <div className="truncate text-xs text-muted">{data?.household?.name}</div>
          </div>
          <button className={linkBase + " w-full"} onClick={() => logout.mutate()}>
            <LogOut size={18} />
            Sign out
          </button>
        </div>
      </aside>

      <main className="min-w-0 flex-1 overflow-y-auto pb-20 md:pb-0">
        <Outlet />
      </main>

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
      </nav>
    </div>
  );
}
