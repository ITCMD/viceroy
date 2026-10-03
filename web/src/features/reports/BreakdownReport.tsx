import { useNavigate } from "@tanstack/react-router";
import clsx from "clsx";
import { ChevronRight, ListFilter } from "lucide-react";
import { useEffect, useMemo } from "react";
import { Legend } from "@/components/charts/Legend";
import { SeriesChart, type Series } from "@/components/charts/SeriesChart";
import { Treemap } from "@/components/charts/Treemap";
import { useChartTokens, type ChartTokens } from "@/components/charts/tokens";
import { Button, Card, CategoryIcon, MoneyText, StatTile, withIcon } from "@/components/ui";
import type { ChatContext } from "@/features/chat/api";
import { txnSearch } from "@/features/transactions/api";
import { shade } from "@/lib/color";
import { TOP_LINES, type Line, type RangeWindow, type Report, type ReportTree, type TreeNode } from "./api";
import { contextTree, treeLink } from "./CashFlowReport";
import { BreakdownTable, NoData, barWidthFor, dollars, pct, useLabels, useSessionState } from "./shared";
import { childColor, sectionColor } from "./treeColors";

export type BreakdownChart = "time" | "breakdown";

/** Colors the top lines in fixed categorical order; the rest fold into a muted "Other". */
function colorLines(lines: Line[], t: ChartTokens) {
  const top = lines.filter((l) => l.total > 0).slice(0, TOP_LINES);
  const colors = new Map(top.map((l, i) => [l.key, t.series[i]]));
  const rest = lines.filter((l) => !colors.has(l.key));
  return { top, rest, colors };
}

export function BreakdownReport({
  report,
  tree,
  side,
  chart,
  win,
  onContext,
}: {
  report: Report;
  tree: ReportTree | undefined;
  side: "spending" | "income";
  chart: BreakdownChart;
  win: RangeWindow;
  onContext: (c: ChatContext) => void;
}) {
  const t = useChartTokens();
  const labels = useLabels(report);
  const s = report[side];
  const { top, rest, colors } = useMemo(() => colorLines(s.lines, t), [s, t]);
  const width = barWidthFor(labels.length);
  const series = useMemo<Series[]>(() => {
    const out: Series[] = [];
    // A slim income bar left of each spending stack shows what came in against what went out.
    if (side === "spending") out.push({ name: "Income", values: report.income.values, color: t.positive, barWidth: Math.max(6, Math.round(width / 4)) });
    out.push(...top.map((l) => ({ name: l.name, values: l.values, color: colors.get(l.key)!, stack: "a", barWidth: width })));
    if (rest.length) {
      out.push({
        name: "Other",
        values: report.buckets.map((_, i) => rest.reduce((a, l) => a + l.values[i], 0)),
        color: t.muted,
        stack: "a",
        barWidth: width,
      });
    }
    return out;
  }, [side, top, rest, colors, report, t, width]);
  const title = side === "spending" ? "Spending" : "Income";
  const avg = report.buckets.length ? Math.round(s.total / report.buckets.length) : 0;
  const per = { month: "month", quarter: "quarter", year: "year" }[report.interval];

  useEffect(() => {
    onContext({
      title: `${title.toLowerCase()} · ${win.label}`,
      page: `Reports › ${title}`,
      suggestions:
        side === "spending"
          ? ["What are my biggest expenses?", "Where could I cut back?", "Is anything unusual this period?", "How does spending compare to income?"]
          : ["Where does my income come from?", "Is my income steady?", "How much did I earn per month on average?", "What changed recently?"],
      data: {
        period: { label: win.label, from: report.from, to: report.to, interval: report.interval },
        total: dollars(s.total),
        income_total: dollars(report.income.total),
        per_period: report.buckets.map((b, i) => ({ period: b.key, [side]: dollars(s.values[i]), ...(side === "spending" ? { income: dollars(report.income.values[i]) } : {}) })),
        [`by_${report.by}`]: s.lines.slice(0, 60).map((l) => ({ name: l.name, total: dollars(l.total), per_period: l.values.map(dollars) })),
        breakdown: !tree
          ? []
          : side === "spending"
            ? contextTree(tree.spending)
            : (tree.income.children ?? []).map((c) => ({
                category: c.name,
                total: dollars(c.total),
                top_sources: (c.children ?? []).slice(0, 8).map((m) => ({ name: m.name, total: dollars(m.total), transactions: m.count })),
              })),
      },
    });
  }, [onContext, title, side, win.label, report, s, tree]);

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
      {chart === "time" ? (
        <>
          <Card title={title}>
            {s.lines.length === 0 ? (
              <NoData />
            ) : (
              <>
                <Legend items={series} />
                <SeriesChart labels={labels} series={series} label={`${title} over time chart`} height={300} hideZero barMaxWidth={width} />
              </>
            )}
          </Card>
          <Card>
            <BreakdownTable lines={s.lines} total={s.total} buckets={report.buckets.length} colors={colors} otherColor={t.muted} per={per} />
          </Card>
        </>
      ) : !tree ? (
        <Card>
          <div className="h-96" />
        </Card>
      ) : (
        <SpendingMap root={side === "spending" ? tree.spending : tree.income} side={side} win={win} />
      )}
    </>
  );
}

type Level = { node: TreeNode; color: string };

/**
 * The spending map: boxes sized by amount. Spending starts at its sections (category groups,
 * Contributions, Uncategorized), then categories, then merchants; a merchant opens its
 * transactions. Income starts at its categories.
 */
function SpendingMap({ root, side, win }: { root: TreeNode; side: "spending" | "income"; win: RangeWindow }) {
  const t = useChartTokens();
  const navigate = useNavigate();
  const [path, setPath] = useSessionState<string[]>(`viceroy.reports.path.${side}`, []);
  // Across date ranges the drill-down stays put while that box still exists.
  const levels = useMemo(() => {
    const out: Level[] = [{ node: root, color: side === "income" ? t.positive : t.muted }];
    for (const key of path) {
      const cur = out.at(-1)!;
      const kids = cur.node.children ?? [];
      const k = kids.findIndex((c) => c.key === key);
      if (k < 0) break;
      const color = out.length === 1 && side === "spending" ? sectionColor(kids[k], t, k) : out.length === 1 ? shade(t.positive, [0, 0.18, -0.15, 0.32, -0.28][k % 5]) : childColor(cur.color, k);
      out.push({ node: kids[k], color });
    }
    return out;
  }, [root, path, side, t]);
  useEffect(() => {
    if (levels.length - 1 < path.length) setPath(path.slice(0, levels.length - 1));
  }, [levels, path]);

  const cur = levels.at(-1)!;
  const parent = levels.at(-2)?.node;
  const kids = (cur.node.children ?? []).filter((c) => c.total > 0);
  const colorOf = (c: TreeNode, k: number) =>
    levels.length === 1 ? (side === "spending" ? sectionColor(c, t, k) : shade(t.positive, [0, 0.18, -0.15, 0.32, -0.28][k % 5])) : childColor(cur.color, k);
  const items = kids.map((c, k) => ({ key: c.key, label: c.name, icon: c.icon, value: c.total, color: colorOf(c, k) }));
  const select = (key: string) => {
    const c = kids.find((k) => k.key === key);
    if (!c) return;
    if (c.kind === "merchant") {
      const link = treeLink(c, cur.node, win, side);
      if (link) navigate({ to: "/transactions" as string, search: txnSearch(link) as never });
      return;
    }
    setPath([...path, key]);
  };
  const here = levels.length > 1 ? treeLink(cur.node, parent, win, side) : null;
  const nextIsMerchants = kids[0]?.kind === "merchant";
  const title = side === "spending" ? "Where it went" : "Where it came from";

  return (
    <>
      <Card
        title={
          <nav aria-label="Breakdown level" className="flex min-w-0 flex-wrap items-center gap-1" data-testid="treemap-crumbs">
            {levels.map((l, i) => (
              <span key={l.node.key} className="flex items-center gap-1">
                {i > 0 && <ChevronRight size={14} className="text-muted" aria-hidden />}
                {i < levels.length - 1 ? (
                  <button type="button" className="text-muted hover:text-text" onClick={() => setPath(path.slice(0, i))}>
                    {i === 0 ? title : l.node.name}
                  </button>
                ) : (
                  <span>{i === 0 ? title : withIcon(l.node.icon, l.node.name)}</span>
                )}
              </span>
            ))}
          </nav>
        }
        action={
          here && (
            <Button variant="ghost" size="sm" onClick={() => navigate({ to: "/transactions" as string, search: txnSearch(here) as never })}>
              <ListFilter size={14} /> Transactions
            </Button>
          )
        }
      >
        {kids.length === 0 ? (
          <NoData />
        ) : (
          <>
            <Treemap
              items={items}
              total={cur.node.total}
              height={440}
              label={`${title} map`}
              onSelect={select}
              selectHint={nextIsMerchants ? "Show transactions" : "Show what's inside"}
            />
            <p className="mt-2 text-xs text-muted">{nextIsMerchants ? "Click a merchant to see its transactions." : "Click a box to see what's inside."}</p>
          </>
        )}
      </Card>
      <Card>
        <table className="w-full text-[13px]" data-testid="treemap-table">
          <thead>
            <tr className="text-left text-xs text-muted">
              <th className="pb-2 font-medium">{nextIsMerchants ? "Merchant" : levels.length === 1 && side === "spending" ? "Section" : "Category"}</th>
              <th className="hidden pb-2 text-right font-medium sm:table-cell">Transactions</th>
              <th className="pb-2 text-right font-medium">Share</th>
              <th className="pb-2 text-right font-medium">Total</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {kids.map((c, k) => (
              <tr key={c.key}>
                <td className="py-1.5 pr-2">
                  <button type="button" className="flex w-full min-w-0 items-center gap-2 text-left hover:text-accent" onClick={() => select(c.key)}>
                    <span aria-hidden className="size-2 shrink-0 rounded-sm" style={{ background: items[k].color }} />
                    {c.icon && <CategoryIcon icon={c.icon} size="sm" />}
                    <span className="truncate">{c.name}</span>
                  </button>
                </td>
                <td className="hidden py-1.5 text-right text-muted tabular sm:table-cell">{c.count}</td>
                <td className={clsx("py-1.5 text-right text-muted tabular")}>{pct(c.total, cur.node.total)}</td>
                <td className="py-1.5 pl-2 text-right font-medium">
                  <MoneyText cents={c.total} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
    </>
  );
}
