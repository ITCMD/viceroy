import { LineChart } from "echarts/charts";
import { GridComponent, TooltipComponent } from "echarts/components";
import * as echarts from "echarts/core";
import { SVGRenderer } from "echarts/renderers";
import { useEffect, useRef } from "react";
import { formatMoney } from "@/lib/format";
import { useChartTokens } from "./tokens";

echarts.use([LineChart, GridComponent, TooltipComponent, SVGRenderer]);

const compact = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", notation: "compact", maximumFractionDigits: 1 });
const shortDate = (d: string) => new Date(d + "T00:00:00").toLocaleDateString("en-US", { month: "short", day: "numeric" });

/** Single-series money-over-time area chart with a crosshair tooltip. Values are cents. */
export function AreaChart({ points, label, height = 220 }: { points: { date: string; value: number }[]; label: string; height?: number }) {
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
          boundaryGap: false,
          data: points.map((p) => p.date),
          axisLine: { lineStyle: { color: t.border } },
          axisTick: { show: false },
          axisLabel: { color: t.muted, fontSize: 11, formatter: shortDate, hideOverlap: true },
        },
        yAxis: {
          type: "value",
          scale: true,
          splitNumber: 3,
          axisLabel: { color: t.muted, fontSize: 11, formatter: (v: number) => compact.format(v / 100) },
          splitLine: { lineStyle: { color: t.border, type: "dashed" } },
        },
        tooltip: {
          trigger: "axis",
          axisPointer: { type: "line", lineStyle: { color: t.muted, type: "dashed" } },
          backgroundColor: t.surface,
          borderColor: t.border,
          textStyle: { color: t.text, fontSize: 12 },
          formatter: (ps: { axisValue: string; value: number }[]) => {
            const p = ps[0];
            return `<div style="color:${t.muted}">${shortDate(p.axisValue)}</div><b>${label}: ${formatMoney(p.value)}</b>`;
          },
        },
        series: [
          {
            type: "line",
            name: label,
            data: points.map((p) => p.value),
            showSymbol: points.length < 2, // a lone point would otherwise be invisible
            symbolSize: 8,
            lineStyle: { width: 2, color: t.accent },
            itemStyle: { color: t.accent, borderColor: t.surface, borderWidth: 2 },
            areaStyle: {
              color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
                { offset: 0, color: t.accent + "33" },
                { offset: 1, color: t.accent + "00" },
              ]),
            },
          },
        ],
      },
      true,
    );
  }, [points, label, t]);

  return <div ref={el} style={{ height }} role="img" aria-label={`${label} chart`} />;
}
