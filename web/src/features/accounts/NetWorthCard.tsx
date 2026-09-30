import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { useMemo, useState } from "react";
import { AreaChart } from "@/components/charts/AreaChart";
import { Card, MoneyText, Segmented } from "@/components/ui";
import { netWorthQuery } from "./api";

const ranges = [
  { days: 30, label: "1M" },
  { days: 90, label: "3M" },
  { days: 365, label: "1Y" },
  { days: 1825, label: "5Y" },
];

/** Net worth headline, change over the range and history chart (Accounts tab and Dashboard). */
export function NetWorthCard() {
  const [days, setDays] = useState(90);
  const { data } = useQuery(netWorthQuery(days));
  const points = useMemo(() => (data?.points ?? []).map((p) => ({ date: p.date, value: p.net })), [data]);
  const last = points.at(-1)?.value ?? 0;
  const first = points[0]?.value ?? 0;
  const change = last - first;
  const label = ranges.find((r) => r.days === days)!.label;

  return (
    <Card>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="text-[13px] font-medium text-muted">Net worth</div>
          <MoneyText cents={last} className="text-3xl font-semibold tracking-tight" />
          <div className={clsx("mt-0.5 text-[13px] tabular", change > 0 ? "text-positive" : change < 0 ? "text-negative" : "text-muted")}>
            {change >= 0 ? "+" : "−"}
            <MoneyText cents={Math.abs(change)} /> <span className="text-muted">over {label}</span>
          </div>
        </div>
        <Segmented label="Time range" value={days} onChange={setDays} items={ranges.map((r) => ({ value: r.days, label: r.label }))} />
      </div>
      <div className="mt-3">
        <AreaChart points={points} label="Net worth" />
      </div>
    </Card>
  );
}
