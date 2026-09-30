import { BarChart, LineChart } from "echarts/charts";
import { GridComponent, TooltipComponent } from "echarts/components";
import * as echarts from "echarts/core";
import { SVGRenderer } from "echarts/renderers";
import { useEffect } from "react";
import { formatMoney } from "@/lib/format";
import { useChartTokens } from "./tokens";
import { useEChart } from "./useEChart";

echarts.use([BarChart, LineChart, GridComponent, TooltipComponent, SVGRenderer]);

const compact = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", notation: "compact", maximumFractionDigits: 1 });

export type Series = {
  name: string;
  values: (number | null)[];
  color: string;
  type?: "bar" | "line";
  /** Bars sharing a stack id are stacked. */
  stack?: string;
  dashed?: boolean;
};

/**
 * Several money series (cents) over shared labels: grouped or stacked bars, lines, or a mix
 * (e.g. income/expense bars with a net line). One y-axis. Legends are rendered by the caller.
 */
export function SeriesChart({
  labels,
  series,
  height = 240,
  label,
  hideZero = false,
}: {
  labels: string[];
  series: Series[];
  height?: number;
  label: string;
  /** Leave zero values out of the tooltip (useful for many stacked series). */
  hideZero?: boolean;
}) {
  const { el, chart } = useEChart();
  const t = useChartTokens();

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
          splitNumber: 4,
          minInterval: 100,
          axisLabel: { color: t.muted, fontSize: 11, formatter: (v: number) => compact.format(v / 100) },
          splitLine: { lineStyle: { color: t.border, type: "dashed" } },
        },
        tooltip: {
          trigger: "axis",
          axisPointer: { type: series.some((s) => s.type !== "line") ? "shadow" : "line", shadowStyle: { color: t.muted + "14" }, lineStyle: { color: t.muted } },
          backgroundColor: t.surface,
          borderColor: t.border,
          textStyle: { color: t.text, fontSize: 12 },
          confine: true,
          formatter: (ps: { axisValue: string; seriesName: string; value: number | null; color: string }[]) =>
            `<div style="color:${t.muted}">${ps[0]?.axisValue ?? ""}</div>` +
            ps
              .filter((p) => p.value != null && !(hideZero && p.value === 0))
              .map(
                (p) =>
                  `<div style="display:flex;align-items:center;gap:6px"><span style="width:8px;height:8px;border-radius:2px;background:${p.color}"></span>${p.seriesName}<b style="margin-left:auto;padding-left:12px">${formatMoney(p.value!)}</b></div>`,
              )
              .join(""),
        },
        series: series.map((s) =>
          s.type === "line"
            ? {
                type: "line",
                name: s.name,
                data: s.values,
                symbol: "circle",
                symbolSize: 6,
                showSymbol: labels.length <= 16,
                connectNulls: false,
                itemStyle: { color: s.color },
                lineStyle: { width: 2, color: s.color, type: s.dashed ? "dashed" : "solid" },
                z: 3,
              }
            : {
                type: "bar",
                name: s.name,
                stack: s.stack,
                data: s.values,
                barMaxWidth: 28,
                itemStyle: { color: s.color, borderColor: t.surface, borderWidth: s.stack ? 1 : 0, borderRadius: s.stack ? 0 : [4, 4, 0, 0] },
              },
        ),
      },
      true,
    );
  }, [chart, labels, series, hideZero, t]);

  return <div ref={el} style={{ height }} role="img" aria-label={label} />;
}
