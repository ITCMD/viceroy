import { BarChart as Bars, LineChart } from "echarts/charts";
import { GridComponent, TooltipComponent } from "echarts/components";
import * as echarts from "echarts/core";
import { SVGRenderer } from "echarts/renderers";
import { useEffect, useRef } from "react";
import { formatMoney } from "@/lib/format";
import { useChartTokens } from "./tokens";

echarts.use([Bars, LineChart, GridComponent, TooltipComponent, SVGRenderer]);

const compact = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", notation: "compact", maximumFractionDigits: 1 });

/**
 * Money bars per label (cents), with an optional dashed target line (e.g. the budget).
 * `highlight` marks one bar (the current period) in the accent color; the rest are muted.
 */
export function BarChart({
  labels,
  values,
  target,
  valueLabel,
  targetLabel = "Budget",
  highlight,
  height = 180,
}: {
  labels: string[];
  values: number[];
  target?: number[];
  valueLabel: string;
  targetLabel?: string;
  highlight?: number;
  height?: number;
}) {
  const el = useRef<HTMLDivElement>(null);
  const chart = useRef<echarts.ECharts | null>(null);
  const t = useChartTokens();

  useEffect(() => {
    if (!el.current) return;
    const c = echarts.init(el.current, undefined, { renderer: "svg" });
    chart.current = c;
    const ro = new ResizeObserver(() => c.resize());
    ro.observe(el.current);
    return () => {
      ro.disconnect();
      c.dispose();
    };
  }, []);

  useEffect(() => {
    chart.current?.setOption(
      {
        animationDuration: 300,
        grid: { left: 4, right: 8, top: 12, bottom: 4, containLabel: true },
        xAxis: {
          type: "category",
          data: labels,
          axisLine: { lineStyle: { color: t.border } },
          axisTick: { show: false },
          axisLabel: { color: t.muted, fontSize: 11 },
        },
        yAxis: {
          type: "value",
          splitNumber: 3,
          minInterval: 100, // cents: never repeat "$0" labels on an empty chart
          axisLabel: { color: t.muted, fontSize: 11, formatter: (v: number) => compact.format(v / 100) },
          splitLine: { lineStyle: { color: t.border, type: "dashed" } },
        },
        tooltip: {
          trigger: "axis",
          axisPointer: { type: "shadow", shadowStyle: { color: t.muted + "14" } },
          backgroundColor: t.surface,
          borderColor: t.border,
          textStyle: { color: t.text, fontSize: 12 },
          formatter: (ps: { axisValue: string; seriesName: string; value: number }[]) =>
            `<div style="color:${t.muted}">${ps[0].axisValue}</div>` +
            ps.map((p) => `<div><b>${p.seriesName}: ${formatMoney(p.value)}</b></div>`).join(""),
        },
        series: [
          {
            type: "bar",
            name: valueLabel,
            barMaxWidth: 28,
            data: values.map((v, i) => ({
              value: v,
              itemStyle: { color: i === highlight ? t.accent : t.muted + "66", borderRadius: [4, 4, 0, 0] },
            })),
          },
          ...(target
            ? [
                {
                  type: "line",
                  name: targetLabel,
                  step: "middle",
                  data: target,
                  symbol: "none",
                  lineStyle: { width: 1.5, color: t.text, type: "dashed" },
                },
              ]
            : []),
        ],
      },
      true,
    );
  }, [labels, values, target, valueLabel, targetLabel, highlight, t]);

  return <div ref={el} style={{ height }} role="img" aria-label={`${valueLabel} chart`} />;
}
