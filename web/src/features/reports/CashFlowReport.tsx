import { useNavigate } from "@tanstack/react-router";
import { useEffect, useMemo } from "react";
import { Legend } from "@/components/charts/Legend";
import { SankeyChart, type FlowLink, type FlowNode } from "@/components/charts/SankeyChart";
import { SeriesChart, type Series } from "@/components/charts/SeriesChart";
import { useChartTokens } from "@/components/charts/tokens";
import { Card, MoneyText, StatTile, withIcon } from "@/components/ui";
import type { ChatContext } from "@/features/chat/api";
import { txnSearch, type TxnLink } from "@/features/transactions/api";
import { shade } from "@/lib/color";
import { formatMoney } from "@/lib/format";
import type { RangeWindow, Report, ReportTree, TreeNode } from "./api";
import { BreakdownTable, NoData, barWidthFor, dollars, pct, useLabels } from "./shared";
import { childColor, sectionColor } from "./treeColors";

export type CashFlowChart = "flow" | "time";

/** Most boxes per column of the flow diagram before the rest fold into "Other". */
const MAX_INCOME = 8;
const MAX_PER_SECTION = 6;

/** Transactions behind a tree box over the report's dates. */
export function treeLink(n: TreeNode, parent: TreeNode | undefined, win: RangeWindow, side: "income" | "spending"): TxnLink | null {
  const dates: TxnLink = { from: win.from === "all" ? undefined : win.from, to: win.to || undefined };
  const dir: TxnLink = side === "income" ? { direction: "in" } : {};
  switch (n.kind) {
    case "group":
      return { ...dates, group: n.id };
    case "category":
      return { ...dates, category: n.id };
    case "goal":
      return { ...dates, goal: n.id };
    case "contributions":
      return null;
    case "uncategorized":
      return { ...dates, ...dir, uncategorized: true, direction: side === "income" ? "in" : "out" };
    case "merchant":
      if (!parent) return null;
      return { ...(treeLink(parent, undefined, win, side) ?? dates), merchant: n.name };
  }
  return null;
}

export function CashFlowReport({
  report,
  tree,
  win,
  chart,
  onContext,
}: {
  report: Report;
  tree: ReportTree | undefined;
  win: RangeWindow;
  chart: CashFlowChart;
  onContext: (c: ChatContext) => void;
}) {
  const t = useChartTokens();
  const labels = useLabels(report);
  const navigate = useNavigate();
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
  const flow = useMemo(() => (tree ? buildFlow(tree, t) : null), [tree, t]);

  useEffect(() => {
    onContext({
      title: `cash flow · ${win.label}`,
      page: "Reports › Cash flow",
      suggestions: ["Where is most of my money going?", "How could I save more?", "What changed compared to before?", "Which merchants cost me the most?"],
      data: {
        period: { label: win.label, from: report.from, to: report.to },
        totals: { income: dollars(income.total), expenses: dollars(spending.total), net_savings: dollars(net), savings_rate: income.total > 0 ? pct(Math.max(net, 0), income.total) : null },
        per_period: report.buckets.map((b, i) => ({ period: b.key, income: dollars(income.values[i]), expenses: dollars(spending.values[i]) })),
        income_by_category: tree?.income.children?.map((c) => ({ name: c.name, total: dollars(c.total) })) ?? [],
        spending: tree ? contextTree(tree.spending) : [],
      },
    });
  }, [onContext, win.label, report, tree, income, spending, net]);

  const open = (key: string) => {
    if (!tree || !flow) return;
    const hit = flow.lookup.get(key);
    if (!hit) return;
    const link = treeLink(hit.node, hit.parent, win, hit.side);
    if (link) navigate({ to: "/transactions" as string, search: txnSearch(link) as never });
  };

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
        ) : chart === "time" ? (
          <>
            <Legend items={series} />
            <SeriesChart labels={labels} series={series} label="Cash flow chart" height={280} barMaxWidth={barWidthFor(labels.length) / 2} />
          </>
        ) : !flow ? (
          <div className="h-96" />
        ) : (
          <div className="-mx-1 overflow-x-auto px-1" data-testid="cashflow-sankey">
            <div className="min-w-[640px]">
              <SankeyChart nodes={flow.nodes} links={flow.links} height={flow.height} label="Cash flow diagram" onNodeClick={open} />
            </div>
            <p className="mt-2 text-xs text-muted">Click a box to see its transactions.</p>
          </div>
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

/** The spending tree for the model: sections > categories > top merchants. */
export function contextTree(root: TreeNode) {
  return (root.children ?? []).map((s) => ({
    section: s.name,
    total: dollars(s.total),
    items: (s.children ?? []).slice(0, 40).map((c) => ({
      name: c.name,
      total: dollars(c.total),
      transactions: c.count,
      top_merchants: (c.children ?? []).slice(0, 8).map((m) => ({ name: m.name, total: dollars(m.total), transactions: m.count })),
    })),
  }));
}

type Hit = { node: TreeNode; parent?: TreeNode; side: "income" | "spending" };

/**
 * Income sources > Income > spending sections (plus what was saved) > categories, as boxes
 * and flows. When spending beat income, "From savings" makes up the difference on the left.
 */
function buildFlow(tree: ReportTree, t: ReturnType<typeof useChartTokens>) {
  const inTotal = Math.max(tree.income.total, 0);
  const outTotal = Math.max(tree.spending.total, 0);
  const base = Math.max(inTotal, outTotal);
  const sub = (v: number) => `${formatMoney(v, { whole: true })} · ${pct(v, base)}`;
  const nodes: FlowNode[] = [];
  const links: FlowLink[] = [];
  const lookup = new Map<string, Hit>();
  const hub = "hub";

  const sources = (tree.income.children ?? []).filter((n) => n.total > 0);
  const shown = sources.slice(0, MAX_INCOME);
  const rest = sources.slice(MAX_INCOME).reduce((a, n) => a + n.total, 0);
  shown.forEach((n, k) => {
    const key = `in:${n.key}`;
    nodes.push({ key, label: withIcon(n.icon, n.name), sub: sub(n.total), color: shade(t.positive, [0, 0.18, -0.15, 0.32, -0.28][k % 5]), depth: 0, clickable: true });
    links.push({ source: key, target: hub, value: n.total });
    lookup.set(key, { node: n, side: "income" });
  });
  if (rest > 0) {
    nodes.push({ key: "in:other", label: "Other income", sub: sub(rest), color: shade(t.positive, 0.45), depth: 0 });
    links.push({ source: "in:other", target: hub, value: rest });
  }
  if (outTotal > inTotal) {
    nodes.push({ key: "in:savings", label: "From savings", sub: sub(outTotal - inTotal), color: t.muted, depth: 0 });
    links.push({ source: "in:savings", target: hub, value: outTotal - inTotal });
  }
  nodes.push({ key: hub, label: outTotal > inTotal ? "Money in" : "Income", sub: sub(base), color: t.positive, depth: 1 });

  let leaves = 0;
  const sections = (tree.spending.children ?? []).filter((n) => n.total > 0);
  sections.forEach((s, i) => {
    const color = sectionColor(s, t, i);
    const key = `s:${s.key}`;
    nodes.push({ key, label: withIcon(s.icon, s.name), sub: sub(s.total), color, depth: 2, clickable: s.kind !== "contributions" });
    links.push({ source: hub, target: key, value: s.total });
    lookup.set(key, { node: s, side: "spending" });
    if (s.kind === "uncategorized") {
      leaves++;
      return;
    }
    const kids = (s.children ?? []).filter((c) => c.total > 0);
    const top = kids.slice(0, MAX_PER_SECTION);
    const other = kids.slice(MAX_PER_SECTION).reduce((a, c) => a + c.total, 0);
    top.forEach((c, k) => {
      const ck = `c:${s.key}/${c.key}`;
      nodes.push({ key: ck, label: withIcon(c.icon, c.name), sub: sub(c.total), color: childColor(color, k), depth: 3, clickable: true });
      links.push({ source: key, target: ck, value: c.total });
      lookup.set(ck, { node: c, parent: s, side: "spending" });
    });
    if (other > 0) {
      const ok = `c:${s.key}/other`;
      nodes.push({ key: ok, label: `Other ${s.name.toLowerCase()}`, sub: sub(other), color: shade(color, 0.5), depth: 3 });
      links.push({ source: key, target: ok, value: other });
    }
    leaves += top.length + (other > 0 ? 1 : 0);
  });
  if (inTotal > outTotal) {
    nodes.push({ key: "saved", label: "Saved", sub: sub(inTotal - outTotal), color: t.series[5], depth: 2 });
    links.push({ source: hub, target: "saved", value: inTotal - outTotal });
    leaves++;
  }
  const rows = Math.max(leaves, shown.length + (rest > 0 ? 1 : 0), sections.length + 1);
  return { nodes, links, lookup, height: Math.max(360, rows * 44) };
}
