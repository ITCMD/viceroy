import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { BarChart3 } from "lucide-react";
import { useMemo, useState } from "react";
import { AreaChart } from "@/components/charts/AreaChart";
import { DonutChart } from "@/components/charts/DonutChart";
import { Legend } from "@/components/charts/Legend";
import { SeriesChart, type Series } from "@/components/charts/SeriesChart";
import { useChartTokens, type ChartTokens } from "@/components/charts/tokens";
import { Card, CategoryIcon, EmptyState, MoneyText, PageHeader, Segmented, StatTile, Tabs } from "@/components/ui";
import { groupLabels, netWorthQuery, type AccountGroup } from "@/features/accounts/api";
import { formatMoney } from "@/lib/format";
import { bucketLabel, rangeFrom, rangeItems, reportQuery, TOP_LINES, type By, type Interval, type Line, type RangeKey, type Report } from "./api";

type Tab = "cashflow" | "spending" | "income" | "networth";

const tabs: { value: Tab; label: string }[] = [
  { value: "cashflow", label: "Cash flow" },
  { value: "spending", label: "Spending" },
  { value: "income", label: "Income" },
  { value: "networth", label: "Net worth" },
];

const intervals: { value: Interval; label: string }[] = [
  { value: "month", label: "Monthly" },
  { value: "quarter", label: "Quarterly" },
  { value: "year", label: "Yearly" },
];

const byItems: { value: By; label: string }[] = [
  { value: "category", label: "Category" },
  { value: "group", label: "Group" },
  { value: "merchant", label: "Merchant" },
];

type Chart = "time" | "breakdown";

const TAB_KEY = "viceroy.reports.tab";

function load<T extends string>(key: string, allowed: readonly T[], fallback: T): T {
  try {
    const v = localStorage.getItem(key) as T | null;
    if (v && allowed.includes(v)) return v;
  } catch {
    /* storage unavailable */
  }
  return fallback;
}

function save(key: string, v: string) {
  try {
    localStorage.setItem(key, v);
  } catch {
    /* ignore */
  }
}

export function ReportsPage() {
  const [tab, setTabState] = useState<Tab>(() => load(TAB_KEY, tabs.map((t) => t.value), "cashflow"));
  const [range, setRange] = useState<RangeKey>("6m");
  const [interval, setInterval] = useState<Interval>("month");
  const [by, setBy] = useState<By>("category");
  const [chart, setChart] = useState<Chart>("time");
  const setTab = (t: Tab) => {
    setTabState(t);
    save(TAB_KEY, t);
  };
  const flowTab = tab !== "networth";
  const { data: report } = useQuery({ ...reportQuery(rangeFrom(range), interval, tab === "cashflow" ? "category" : by), enabled: flowTab });

  return (
    <>
      <PageHeader title="Reports" />
      <div className="mx-auto flex max-w-6xl flex-col gap-4 p-4 md:p-6">
        <Tabs value={tab} onChange={setTab} items={tabs} />
        {flowTab && (
          <div className="flex flex-wrap items-center gap-2" data-testid="report-controls">
            <Segmented label="Date range" value={range} onChange={setRange} items={rangeItems} />
            <Segmented label="Interval" value={interval} onChange={setInterval} items={intervals} />
            {tab !== "cashflow" && (
              <>
                <Segmented label="Group by" value={by} onChange={setBy} items={byItems} />
                <Segmented
                  label="Chart type"
                  value={chart}
                  onChange={setChart}
                  items={[
                    { value: "time", label: "Over time" },
                    { value: "breakdown", label: "Breakdown" },
                  ]}
                />
              </>
            )}
          </div>
        )}
        {tab === "networth" ? (
          <NetWorthReport />
        ) : !report ? (
          <Card>
            <div className="h-64" />
          </Card>
        ) : tab === "cashflow" ? (
          <CashFlow report={report} />
        ) : (
          <Breakdown report={report} side={tab} chart={chart} />
        )}
      </div>
    </>
  );
}

function useLabels(report: Report) {
  return useMemo(() => {
    const multiYear = report.buckets.length > 0 && report.buckets[0].start.slice(0, 4) !== report.buckets.at(-1)!.start.slice(0, 4);
    return report.buckets.map((b) => bucketLabel(b, report.interval, multiYear));
  }, [report]);
}

const pct = (part: number, whole: number) => (whole > 0 ? `${((part / whole) * 100).toFixed(part / whole < 0.1 ? 1 : 0)}%` : "—");

// ---- cash flow ----

function CashFlow({ report }: { report: Report }) {
  const t = useChartTokens();
  const labels = useLabels(report);
  const { income, spending } = report;
  const net = income.total - spending.total;
  const series = useMemo<Series[]>(
    () => [
      { name: "Income", values: income.values, color: t.positive },
      { name: "Expenses", values: spending.values, color: t.negative },
      { name: "Net savings", values: income.values.map((v, i) => v - spending.values[i]), color: t.text, type: "line" },
    ],
    [income, spending, t],
  );
  const empty = income.total === 0 && spending.total === 0;

  return (
    <>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4" data-testid="cashflow-stats">
        <StatTile label="Income">
          <MoneyText cents={income.total} whole />
        </StatTile>
        <StatTile label="Expenses">
          <MoneyText cents={spending.total} whole />
        </StatTile>
        <StatTile label="Net savings">
          <MoneyText cents={net} whole className={net < 0 ? "text-negative" : undefined} />
        </StatTile>
        <StatTile label="Savings rate">{income.total > 0 ? pct(Math.max(net, 0), income.total) : "—"}</StatTile>
      </div>
      <Card title="Cash flow">
        {empty ? (
          <NoData />
        ) : (
          <>
            <Legend items={series} />
            <SeriesChart labels={labels} series={series} label="Cash flow chart" height={260} />
          </>
        )}
      </Card>
      <div className="grid gap-4 md:grid-cols-2">
        <Card title="Income">
          <BreakdownTable lines={income.lines} total={income.total} buckets={report.buckets.length} />
        </Card>
        <Card title="Expenses">
          <BreakdownTable lines={spending.lines} total={spending.total} buckets={report.buckets.length} />
        </Card>
      </div>
    </>
  );
}

// ---- spending / income breakdown ----

/** Colors the top lines in fixed categorical order; the rest fold into a muted "Other". */
function colorLines(lines: Line[], t: ChartTokens) {
  const top = lines.filter((l) => l.total > 0).slice(0, TOP_LINES);
  const colors = new Map(top.map((l, i) => [l.key, t.series[i]]));
  const rest = lines.filter((l) => !colors.has(l.key));
  return { top, rest, colors };
}

function Breakdown({ report, side, chart }: { report: Report; side: "spending" | "income"; chart: Chart }) {
  const t = useChartTokens();
  const labels = useLabels(report);
  const s = report[side];
  const { top, rest, colors } = useMemo(() => colorLines(s.lines, t), [s, t]);
  const series = useMemo<Series[]>(() => {
    const out: Series[] = top.map((l) => ({ name: l.name, values: l.values, color: colors.get(l.key)!, stack: "a" }));
    if (rest.length) {
      out.push({
        name: "Other",
        values: report.buckets.map((_, i) => rest.reduce((a, l) => a + l.values[i], 0)),
        color: t.muted,
        stack: "a",
      });
    }
    return out;
  }, [top, rest, colors, report.buckets, t]);
  const slices = useMemo(
    () => [
      ...top.map((l) => ({ name: l.name, value: l.total, color: colors.get(l.key)! })),
      ...(rest.length ? [{ name: "Other", value: rest.reduce((a, l) => a + l.total, 0), color: t.muted }] : []),
    ],
    [top, rest, colors, t],
  );
  const title = side === "spending" ? "Spending" : "Income";
  const avg = report.buckets.length ? Math.round(s.total / report.buckets.length) : 0;
  const per = { month: "month", quarter: "quarter", year: "year" }[report.interval];

  return (
    <>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
        <StatTile label={`Total ${title.toLowerCase()}`}>
          <MoneyText cents={s.total} whole />
        </StatTile>
        <StatTile label={`Average per ${per}`}>
          <MoneyText cents={avg} whole />
        </StatTile>
        <StatTile label={`Top ${{ category: "category", group: "group", merchant: side === "spending" ? "merchant" : "source" }[report.by]}`} className="col-span-2 md:col-span-1">
          <span className="block truncate text-base">{s.lines[0]?.name ?? "—"}</span>
        </StatTile>
      </div>
      <Card title={title}>
        {s.lines.length === 0 ? (
          <NoData />
        ) : chart === "time" ? (
          <>
            <Legend items={series} />
            <SeriesChart labels={labels} series={series} label={`${title} over time chart`} height={280} hideZero />
          </>
        ) : (
          <DonutChart slices={slices} label={`${title} breakdown chart`} height={260} />
        )}
      </Card>
      <Card>
        <BreakdownTable lines={s.lines} total={s.total} buckets={report.buckets.length} colors={colors} otherColor={t.muted} per={per} />
      </Card>
    </>
  );
}

/** Table view of a breakdown: every line with its total, share and average; doubles as the chart legend. */
function BreakdownTable({
  lines,
  total,
  buckets,
  colors,
  otherColor,
  per,
}: {
  lines: Line[];
  total: number;
  buckets: number;
  colors?: Map<string, string>;
  otherColor?: string;
  per?: string;
}) {
  if (lines.length === 0) return <p className="py-4 text-center text-sm text-muted">Nothing in this range.</p>;
  return (
    <table className="w-full text-[13px]" data-testid="breakdown-table">
      <thead>
        <tr className="text-left text-xs text-muted">
          <th className="pb-2 font-medium">Name</th>
          {per && <th className="hidden pb-2 text-right font-medium sm:table-cell">Avg / {per}</th>}
          <th className="pb-2 text-right font-medium">Share</th>
          <th className="pb-2 text-right font-medium">Total</th>
        </tr>
      </thead>
      <tbody className="divide-y divide-border">
        {lines.map((l) => {
          const color = colors ? (colors.get(l.key) ?? otherColor) : undefined;
          const share = total > 0 ? Math.max(0, l.total / total) : 0;
          return (
            <tr key={l.key}>
              <td className="py-2 pr-2">
                <div className="flex min-w-0 items-center gap-2">
                  {color && <span aria-hidden className="size-2 shrink-0 rounded-sm" style={{ background: color }} />}
                  {l.icon && <CategoryIcon icon={l.icon} size="sm" />}
                  <div className="min-w-0 flex-1">
                    <div className="truncate">{l.name}</div>
                    <div className="mt-1 h-1 rounded-full bg-surface-2">
                      <div className="h-1 rounded-full bg-muted/50" style={{ width: `${share * 100}%` }} />
                    </div>
                  </div>
                </div>
              </td>
              {per && (
                <td className="hidden py-2 text-right text-muted tabular sm:table-cell">{formatMoney(Math.round(l.total / Math.max(buckets, 1)), { whole: true })}</td>
              )}
              <td className="py-2 text-right text-muted tabular">{pct(l.total, total)}</td>
              <td className="py-2 pl-2 text-right font-medium">
                <MoneyText cents={l.total} />
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

function NoData() {
  return (
    <EmptyState icon={BarChart3} title="No transactions in this range">
      Try a longer date range.
    </EmptyState>
  );
}

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

function NetWorthReport() {
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
