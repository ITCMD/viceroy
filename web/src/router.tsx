import { ApiDocsPage } from "@/features/api/ApiDocsPage";
import { createRootRouteWithContext, createRoute, createRouter, Outlet, redirect } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { AppShell } from "@/components/AppShell";
import { navItems } from "@/components/nav";
import { LoginPage } from "@/features/auth/LoginPage";
import { SetupPage } from "@/features/auth/SetupPage";
import { AccountsPage } from "@/features/accounts/AccountsPage";
import { SettingsPage } from "@/features/settings/SettingsPage";
import { TransactionsPage } from "@/features/transactions/TransactionsPage";
import { BudgetPage } from "@/features/budget/BudgetPage";
import { GoalsPage } from "@/features/goals/GoalsPage";
import { DashboardPage } from "@/features/dashboard/DashboardPage";
import { ReportsPage } from "@/features/reports/ReportsPage";
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
  "/settings": 7,
};

const pages: Record<string, () => React.ReactNode> = {
  "/": DashboardPage,
  "/reports": ReportsPage,
  "/accounts": AccountsPage,
  "/transactions": TransactionsPage,
  "/budget": BudgetPage,
  "/goals": GoalsPage,
  "/settings": SettingsPage,
};

const pageRoutes = navItems.map((item) =>
  createRoute({
    getParentRoute: () => appRoute,
    path: item.to,
    component: pages[item.to] ?? (() => <ComingSoon item={item} phase={phases[item.to]} />),
  }),
);

// Pages that aren't in the navigation.
const extraRoutes = [createRoute({ getParentRoute: () => appRoute, path: "/settings/api-docs", component: ApiDocsPage })];

const routeTree = rootRoute.addChildren([setupRoute, loginRoute, appRoute.addChildren([...pageRoutes, ...extraRoutes])]);

export function makeRouter(queryClient: QueryClient) {
  return createRouter({ routeTree, context: { queryClient }, defaultPreload: "intent" });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof makeRouter>;
  }
}
