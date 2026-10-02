import { ArrowLeftRight, BarChart3, Landmark, LayoutDashboard, ListFilter, PiggyBank, Repeat, Settings, Target, type LucideIcon } from "lucide-react";
import { ShootingStar } from "./icons/ShootingStar";

/** mobile: in the phone tab bar; the rest sit under its "More" tab. */
export type NavItem = { to: string; label: string; icon: LucideIcon; mobile?: boolean };

/** Single source of truth for navigation; the sidebar and mobile tab bar both render this. */
export const navItems: NavItem[] = [
  { to: "/", label: "Dashboard", icon: LayoutDashboard, mobile: true },
  { to: "/accounts", label: "Accounts", icon: Landmark, mobile: true },
  { to: "/transactions", label: "Transactions", icon: ArrowLeftRight, mobile: true },
  { to: "/budget", label: "Budget", icon: PiggyBank, mobile: true },
  { to: "/recurring", label: "Recurring", icon: Repeat },
  { to: "/rules", label: "Rules", icon: ListFilter },
  { to: "/reports", label: "Reports", icon: BarChart3 },
  { to: "/goals", label: "Goals", icon: Target },
  { to: "/wishlist", label: "Wishlist", icon: ShootingStar },
  { to: "/settings", label: "Settings", icon: Settings },
];
