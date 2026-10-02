import { useQuery } from "@tanstack/react-query";
import { Sparkles } from "lucide-react";
import { useCallback, useState } from "react";
import { Button, Card, PageHeader, Segmented, Tabs } from "@/components/ui";
import { ChatSheet } from "@/features/chat/ChatSheet";
import type { ChatContext } from "@/features/chat/api";
import { rangeItems, rangeWindow, reportQuery, treeQuery, type By, type Interval, type RangeKey } from "./api";
import { BreakdownReport, type BreakdownChart } from "./BreakdownReport";
import { CashFlowReport, type CashFlowChart } from "./CashFlowReport";
import { DebtReport } from "./DebtReport";
import { NetWorthReport } from "./NetWorthReport";
import { PeriodNav, useSessionState } from "./shared";

type Tab = "cashflow" | "spending" | "income" | "networth" | "debt";

const tabs: { value: Tab; label: string }[] = [
  { value: "cashflow", label: "Cash flow" },
  { value: "spending", label: "Spending" },
  { value: "income", label: "Income" },
  { value: "networth", label: "Net worth" },
  { value: "debt", label: "Debt Free Future" },
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

type FlowTab = "cashflow" | "spending" | "income";
/** Each flow tab keeps its own date window: cash flow opens on this month, the others on six. */
type Windows = Record<FlowTab, { range: RangeKey; offset: number }>;
const initialWindows: Windows = { cashflow: { range: "1m", offset: 0 }, spending: { range: "6m", offset: 0 }, income: { range: "6m", offset: 0 } };

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
  const [windows, setWindows] = useSessionState<Windows>("viceroy.reports.windows", initialWindows);
  const [interval, setInterval] = useSessionState<Interval>("viceroy.reports.interval", "month");
  const [by, setBy] = useSessionState<By>("viceroy.reports.by", "category");
  const [chart, setChart] = useSessionState<BreakdownChart>("viceroy.reports.chart", "time");
  const [flowChart, setFlowChart] = useSessionState<CashFlowChart>("viceroy.reports.flow", "flow");
  const [discuss, setDiscuss] = useState(false);
  const [context, setContext] = useState<ChatContext>();
  const onContext = useCallback((c: ChatContext) => setContext(c), []);
  const setTab = (t: Tab) => {
    setTabState(t);
    save(TAB_KEY, t);
  };
  const flowTab = tab === "cashflow" || tab === "spending" || tab === "income";
  const w = windows[flowTab ? tab : "cashflow"];
  const win = rangeWindow(w.range, w.offset);
  const setWindow = (patch: Partial<Windows[FlowTab]>) => flowTab && setWindows((ws) => ({ ...ws, [tab]: { ...ws[tab], ...patch } }));
  const needsTree = tab === "cashflow" ? flowChart === "flow" : chart === "breakdown";
  const { data: report } = useQuery({ ...reportQuery(win.from, interval, tab === "cashflow" ? "category" : by, win.to), enabled: flowTab });
  const { data: tree } = useQuery({ ...treeQuery(win.from, win.to), enabled: flowTab && needsTree });

  return (
    <>
      <PageHeader
        title="Reports"
        actions={
          <Button variant="secondary" size="sm" onClick={() => setDiscuss(true)} disabled={!context} data-testid="discuss">
            <Sparkles size={14} /> Discuss
          </Button>
        }
      />
      <div className="mx-auto flex max-w-6xl flex-col gap-4 p-4 md:p-6">
        <div className="-mx-4 overflow-x-auto px-4 md:mx-0 md:px-0">
          <Tabs value={tab} onChange={setTab} items={tabs} />
        </div>
        {flowTab && (
          <div className="flex flex-wrap items-center justify-between gap-2" data-testid="report-controls">
            <PeriodNav
              label={win.label}
              onPrev={() => setWindow({ offset: w.offset + 1 })}
              onNext={() => setWindow({ offset: Math.max(0, w.offset - 1) })}
              onToday={() => setWindow({ offset: 0 })}
              atStart={w.range === "all"}
              atEnd={w.offset === 0}
            />
            <div className="flex flex-wrap items-center gap-2">
              <Segmented label="Date range" value={w.range} onChange={(range) => setWindow({ range, offset: 0 })} items={rangeItems} />
              {tab === "cashflow" ? (
                <>
                  {flowChart === "time" && <Segmented label="Interval" value={interval} onChange={setInterval} items={intervals} />}
                  <Segmented
                    label="Chart type"
                    value={flowChart}
                    onChange={setFlowChart}
                    items={[
                      { value: "flow", label: "Flow" },
                      { value: "time", label: "Over time" },
                    ]}
                  />
                </>
              ) : (
                <>
                  {chart === "time" && (
                    <>
                      <Segmented label="Interval" value={interval} onChange={setInterval} items={intervals} />
                      <Segmented label="Group by" value={by} onChange={setBy} items={byItems} />
                    </>
                  )}
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
          </div>
        )}
        {tab === "networth" ? (
          <NetWorthReport onContext={onContext} />
        ) : tab === "debt" ? (
          <DebtReport onContext={onContext} />
        ) : !report ? (
          <Card>
            <div className="h-64" />
          </Card>
        ) : tab === "cashflow" ? (
          <CashFlowReport report={report} tree={tree} win={win} chart={flowChart} onContext={onContext} />
        ) : (
          <BreakdownReport report={report} tree={tree} side={tab} chart={chart} win={win} onContext={onContext} />
        )}
      </div>
      <ChatSheet open={discuss} onOpenChange={setDiscuss} context={context} />
    </>
  );
}
