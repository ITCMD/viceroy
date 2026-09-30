import { createRootRouteWithContext, createRoute, createRouter, Outlet, redirect } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { AppShell } from "@/components/AppShell";
import { navItems } from "@/components/nav";
import { LoginPage } from "@/features/auth/LoginPage";
import { SetupPage } from "@/features/auth/SetupPage";
import { AccountsPage } from "@/features/accounts/AccountsPage";
import { SettingsPage } from "@/features/settings/SettingsPage";
import { TransactionsPage } from "@/features/transactions/TransactionsPage";
import { ComingSoon } from "@/features/placeholders/ComingSoon";
import { loadSession } from "@/lib/session";

type Ctx = { queryClient: QueryClient };

const rootRoute = createRootRouteWithContext<Ctx>()({ component: Outlet });

const setupRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/setup",
  beforeLoad: async ({ context }) => {
    const s = await loadSession(context.queryClient);
    if (!s.needs_setup) throw redirect({ to: s.user ? "/" : "/login" });
  },
  component: SetupPage,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  beforeLoad: async ({ context }) => {
    const s = await loadSession(context.queryClient);
    if (s.needs_setup) throw redirect({ to: "/setup" });
    if (s.user) throw redirect({ to: "/" });
  },
  component: LoginPage,
});

const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  beforeLoad: async ({ context }) => {
    const s = await loadSession(context.queryClient);
    if (s.needs_setup) throw redirect({ to: "/setup" });
    if (!s.user) throw redirect({ to: "/login" });
  },
  component: AppShell,
});

const phases: Record<string, number> = {
  "/": 6, "/accounts": 2, "/transactions": 3, "/budget": 5, "/reports": 6, "/goals": 5, "/settings": 7,
};

const pages: Record<string, () => React.ReactNode> = {
  "/accounts": AccountsPage,
  "/transactions": TransactionsPage,
  "/settings": SettingsPage,
};

const pageRoutes = navItems.map((item) =>
  createRoute({
    getParentRoute: () => appRoute,
    path: item.to,
    component: pages[item.to] ?? (() => <ComingSoon item={item} phase={phases[item.to]} />),
  }),
);

const routeTree = rootRoute.addChildren([setupRoute, loginRoute, appRoute.addChildren(pageRoutes)]);

export function makeRouter(queryClient: QueryClient) {
  return createRouter({ routeTree, context: { queryClient }, defaultPreload: "intent" });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof makeRouter>;
  }
}
