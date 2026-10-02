import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { useMemo, useState } from "react";
import { AreaChart } from "@/components/charts/AreaChart";
import { MoneyText, Segmented } from "@/components/ui";
import { accountHistoryQuery, type Account } from "./api";

const ranges = [
  { value: 30, label: "1M" },
  { value: 90, label: "3M" },
  { value: 365, label: "1Y" },
];

/** Small balance-over-time chart for one account. Cards and loans chart the amount owed. */
export function AccountHistory({ account }: { account: Account }) {
  const [days, setDays] = useState(30);
  const { data } = useQuery(accountHistoryQuery(account.id, days));
  const sign = account.is_liability ? -1 : 1;
  const points = useMemo(() => (data?.points ?? []).map((p) => ({ date: p.date, value: sign * p.balance })), [data, sign]);
  const change = (points.at(-1)?.value ?? 0) - (points[0]?.value ?? 0);
  // Owing less is good news.
  const good = account.is_liability ? change < 0 : change > 0;
  const label = ranges.find((r) => r.value === days)!.label;
  return (
    <div className="flex flex-col gap-2" data-testid="account-history">
      <div className="flex items-center justify-between gap-3">
        <div className="text-[13px]">
          <span className="font-semibold">{account.is_liability ? "Amount owed" : "Balance"}</span>
          {points.length > 1 && (
            <span className={clsx("ml-2 tabular", change === 0 ? "text-muted" : good ? "text-positive" : "text-negative")}>
              {change >= 0 ? "+" : "−"}
              <MoneyText cents={Math.abs(change)} /> <span className="text-muted">over {label}</span>
            </span>
          )}
        </div>
        <Segmented label="History range" value={days} onChange={setDays} items={ranges} />
      </div>
      {data && points.length === 0 ? (
        <p className="py-6 text-center text-[13px] text-muted">No balance history yet.</p>
      ) : (
        <AreaChart points={points} label={account.is_liability ? "Owed" : "Balance"} height={140} />
      )}
    </div>
  );
}
