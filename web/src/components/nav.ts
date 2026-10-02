import { ArrowLeftRight, BarChart3, Landmark, LayoutDashboard, PiggyBank, Repeat, Settings, Target, type LucideIcon } from "lucide-react";

export type NavItem = { to: string; label: string; icon: LucideIcon; mobile?: boolean };

/** Single source of truth for navigation; the sidebar and mobile tab bar both render this. */
export const navItems: NavItem[] = [
  { to: "/", label: "Dashboard", icon: LayoutDashboard, mobile: true },
  { to: "/accounts", label: "Accounts", icon: Landmark, mobile: true },
  { to: "/transactions", label: "Transactions", icon: ArrowLeftRight, mobile: true },
  { to: "/budget", label: "Budget", icon: PiggyBank, mobile: true },
  { to: "/recurring", label: "Recurring", icon: Repeat },
  { to: "/reports", label: "Reports", icon: BarChart3 },
  { to: "/goals", label: "Goals", icon: Target },
  { to: "/settings", label: "Settings", icon: Settings, mobile: true },
];
