import { CalendarCheck, CalendarClock, CircleCheck } from "lucide-react";
import { Badge } from "@/components/ui";
import { formatMoney } from "@/lib/format";
import type { Bill } from "./api";

const day = (d: string) => new Date(d + "T00:00:00").toLocaleDateString("en-US", { month: "short", day: "numeric" });

function daysUntil(d: string) {
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  return Math.round((new Date(d + "T00:00:00").getTime() - today.getTime()) / 86_400_000);
}

/** Payment state read from bank emails: due (red when close and nothing is scheduled), scheduled, paid. */
export function BillBadges({ bill }: { bill: Bill | null }) {
  if (!bill) return null;
  const due = bill.due_date;
  const urgent = !!due && !bill.scheduled_date && daysUntil(due) <= 3;
  const amount = (c: number | null) => (c != null ? ` · ${formatMoney(c)}` : "");
  return (
    <span className="inline-flex flex-wrap items-center gap-1" data-testid="bill-badges">
      {due && (
        <span title={`Payment due ${day(due)}${amount(bill.due_cents)}${bill.minimum_cents != null ? `, minimum ${formatMoney(bill.minimum_cents)}` : ""}`}>
          <Badge tone={urgent ? "negative" : "warning"}>
            <CalendarClock size={11} /> Due {day(due)}
          </Badge>
        </span>
      )}
      {bill.scheduled_date && (
        <span title={`Payment scheduled ${day(bill.scheduled_date)}${amount(bill.scheduled_cents)}`}>
          <Badge tone="positive">
            <CalendarCheck size={11} /> Scheduled {day(bill.scheduled_date)}
          </Badge>
        </span>
      )}
      {bill.paid_date && !due && daysUntil(bill.paid_date) >= -7 && (
        <span title={`Payment made ${day(bill.paid_date)}${amount(bill.paid_cents)}`}>
          <Badge tone="positive">
            <CircleCheck size={11} /> Paid {day(bill.paid_date)}
          </Badge>
        </span>
      )}
    </span>
  );
}
