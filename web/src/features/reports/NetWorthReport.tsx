import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { BarChart3 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { AreaChart } from "@/components/charts/AreaChart";
import { Legend } from "@/components/charts/Legend";
import { SeriesChart, type Series } from "@/components/charts/SeriesChart";
import { useChartTokens, type ChartTokens } from "@/components/charts/tokens";
import { Card, EmptyState, MoneyText, Segmented, StatTile } from "@/components/ui";
import { groupLabels, netWorthQuery, type AccountGroup } from "@/features/accounts/api";
import type { ChatContext } from "@/features/chat/api";
import { formatMoney } from "@/lib/format";
import { dollars } from "./shared";

// ---- net worth ----

const nwRanges = [
  { value: 30, label: "1M" },
  { value: 90, label: "3M" },
  { value: 182, label: "6M" },
  { value: 365, label: "1Y" },
  { value: 1825, label: "5Y" },
];

const groupOrder: AccountGroup[] = ["cash", "investments", "other", "credit", "loans"];

function groupColor(g: AccountGroup, t: ChartTokens) {
  return { cash: t.series[0], investments: t.series[2], other: t.series[3], credit: t.series[1], loans: t.series[6] }[g];
}

export function NetWorthReport({ onContext }: { onContext: (c: ChatContext) => void }) {
  const t = useChartTokens();
  const [days, setDays] = useState(365);
  const [view, setView] = useState<"net" | "type">("net");
  const { data } = useQuery(netWorthQuery(days));
  const points = useMemo(() => data?.points ?? [], [data]);
  const first = points[0];
  const last = points.at(-1);

  // Stacked-by-type view samples one point per week (short ranges) or per month.
  const sampled = useMemo(() => {
    if (days <= 90) return points.filter((_, i) => (points.length - 1 - i) % 7 === 0);
    return points.filter((p, i) => i === points.length - 1 || p.date.slice(0, 7) !== points[i + 1].date.slice(0, 7));
  }, [points, days]);
  const present = groupOrder.filter((g) => points.some((p) => (p.groups?.[g] ?? 0) !== 0));
  const series = useMemo<Series[]>(
    () => present.map((g) => ({ name: groupLabels[g], values: sampled.map((p) => p.groups?.[g] ?? 0), color: groupColor(g, t), stack: "a" })),
    [present, sampled, t],
  );
  const labels = sampled.map((p) =>
    new Date(p.date + "T00:00:00").toLocaleDateString("en-US", days <= 90 ? { month: "short", day: "numeric" } : { month: "short", year: "2-digit" }),
  );
  const change = (last?.net ?? 0) - (first?.net ?? 0);
  const rangeLabel = nwRanges.find((r) => r.value === days)!.label;

  useEffect(() => {
    // One point per month (and the latest) keeps the context small.
    const monthly = points.filter((p, i) => i === points.length - 1 || p.date.slice(0, 7) !== points[i + 1].date.slice(0, 7));
    onContext({
      title: `net worth · ${rangeLabel}`,
      page: "Reports › Net worth",
      suggestions: ["How has my net worth changed?", "What's driving the change?", "How are my assets split?", "Am I on track?"],
      data: {
        range: rangeLabel,
        now: last && { date: last.date, net_worth: dollars(last.net), assets: dollars(last.assets), liabilities: dollars(last.liabilities), by_type: Object.fromEntries(Object.entries(last.groups ?? {}).map(([k, v]) => [k, dollars(v)])) },
        change: dollars(change),
        history: monthly.map((p) => ({ date: p.date, net_worth: dollars(p.net), assets: dollars(p.assets), liabilities: dollars(p.liabilities) })),
      },
    });
  }, [onContext, points, last, change, rangeLabel]);

  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <Segmented label="Date range" value={days} onChange={setDays} items={nwRanges} />
        <Segmented
          label="Chart type"
          value={view}
          onChange={setView}
          items={[
            { value: "net", label: "Net worth" },
            { value: "type", label: "By type" },
          ]}
        />
      </div>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatTile label="Net worth">
          <MoneyText cents={last?.net ?? 0} whole />
        </StatTile>
        <StatTile label={`Change over ${rangeLabel}`}>
          <span className={clsx(change > 0 && "text-positive", change < 0 && "text-negative")}>
            {change >= 0 ? "+" : "−"}
            {formatMoney(Math.abs(change), { whole: true })}
          </span>
        </StatTile>
        <StatTile label="Assets">
          <MoneyText cents={last?.assets ?? 0} whole />
        </StatTile>
        <StatTile label="Liabilities">
          <MoneyText cents={last?.liabilities ?? 0} whole />
        </StatTile>
      </div>
      <Card title="Net worth">
        {points.length === 0 ? (
          <EmptyState icon={BarChart3} title="No balance history yet">
            History builds up as accounts sync.
          </EmptyState>
        ) : view === "net" ? (
          <AreaChart points={points.map((p) => ({ date: p.date, value: p.net }))} label="Net worth" height={280} />
        ) : (
          <>
            <Legend items={series} />
            <SeriesChart labels={labels} series={series} label="Net worth by account type chart" height={280} hideZero />
          </>
        )}
      </Card>
      <Card>
        <table className="w-full text-[13px]" data-testid="networth-table">
          <thead>
            <tr className="text-left text-xs text-muted">
              <th className="pb-2 font-medium">Account type</th>
              <th className="pb-2 text-right font-medium">Change</th>
              <th className="pb-2 text-right font-medium">Balance</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {present.map((g) => {
              const now = last?.groups?.[g] ?? 0;
              const diff = now - (first?.groups?.[g] ?? 0);
              return (
                <tr key={g}>
                  <td className="py-2">
                    <span className="flex items-center gap-2">
                      <span aria-hidden className="size-2 rounded-sm" style={{ background: groupColor(g, t) }} />
                      {groupLabels[g]}
                    </span>
                  </td>
                  <td className={clsx("py-2 text-right tabular", diff > 0 ? "text-positive" : diff < 0 ? "text-negative" : "text-muted")}>
                    {diff >= 0 ? "+" : "−"}
                    {formatMoney(Math.abs(diff))}
                  </td>
                  <td className="py-2 text-right font-medium">
                    <MoneyText cents={now} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </Card>
    </>
  );
}
