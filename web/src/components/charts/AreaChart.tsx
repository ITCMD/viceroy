import { LineChart } from "echarts/charts";
import { GridComponent, MarkPointComponent, TooltipComponent } from "echarts/components";
import * as echarts from "echarts/core";
import { SVGRenderer } from "echarts/renderers";
import { useEffect, useRef } from "react";
import { formatMoney } from "@/lib/format";
import { useChartTokens } from "./tokens";

echarts.use([LineChart, GridComponent, TooltipComponent, MarkPointComponent, SVGRenderer]);

const compact = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", notation: "compact", maximumFractionDigits: 1 });
const shortDate = (d: string) => new Date(d + "T00:00:00").toLocaleDateString("en-US", { month: "short", day: "numeric" });

const esc = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);

/** A note pinned to a day on the chart: its icon sits on the line, the label shows on hover. */
export type ChartMarker = { id: number; date: string; label: string; icon: string };
const noMarkers: ChartMarker[] = [];

/** Single-series money-over-time area chart with a crosshair tooltip. Values are cents.
 * Optional markers; onPickDate fires on right-click (or long-press) with the day under the
 * pointer, onMarkerClick when a marker is clicked. */
export function AreaChart({
  points,
  label,
  height = 220,
  markers = noMarkers,
  onPickDate,
  onMarkerClick,
}: {
  points: { date: string; value: number }[];
  label: string;
  height?: number;
  markers?: ChartMarker[];
  onPickDate?: (date: string) => void;
  onMarkerClick?: (id: number) => void;
}) {
  const el = useRef<HTMLDivElement>(null);
  const chart = useRef<echarts.ECharts | null>(null);
  const t = useChartTokens();
  const handlers = useRef({ onPickDate, onMarkerClick, points });
  handlers.current = { onPickDate, onMarkerClick, points };

  useEffect(() => {
    if (!el.current) return;
    const c = echarts.init(el.current, undefined, { renderer: "svg" });
    chart.current = c;
    const ro = new ResizeObserver(() => c.resize());
    ro.observe(el.current);
    c.getZr().on("contextmenu", (e) => {
      const { onPickDate, points } = handlers.current;
      if (!onPickDate || points.length === 0) return;
      (e.event as unknown as Event).preventDefault();
      const [i] = c.convertFromPixel({ seriesIndex: 0 }, [e.offsetX, e.offsetY]) as number[];
      const p = points[Math.max(0, Math.min(points.length - 1, Math.round(i)))];
      if (p) onPickDate(p.date);
    });
    c.on("click", { componentType: "markPoint" }, (p) => {
      const id = (p.data as { id?: number } | undefined)?.id;
      if (id) handlers.current.onMarkerClick?.(id);
    });
    return () => {
      ro.disconnect();
      c.dispose();
    };
  }, []);

  useEffect(() => {
    const index = new Map(points.map((p, i) => [p.date, i]));
    const notesOn = new Map<string, ChartMarker[]>();
    for (const m of markers) if (index.has(m.date)) notesOn.set(m.date, [...(notesOn.get(m.date) ?? []), m]);
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
            const notes = (notesOn.get(p.axisValue) ?? []).map((m) => `<div style="margin-top:4px">${esc(m.icon || "•")} ${esc(m.label)}</div>`).join("");
            return `<div style="color:${t.muted}">${shortDate(p.axisValue)}</div><b>${label}: ${formatMoney(p.value)}</b>${notes}`;
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
            markPoint: {
              symbol: "circle",
              symbolSize: 22,
              animation: false,
              itemStyle: { color: t.surface, borderColor: t.accent, borderWidth: 1.5 },
              label: { show: true, fontSize: 12, color: t.text, formatter: (p: { data?: { icon?: string } }) => p.data?.icon || "•" },
              data: markers
                .filter((m) => index.has(m.date))
                .map((m) => ({ id: m.id, name: m.label, icon: m.icon, coord: [index.get(m.date)!, points[index.get(m.date)!].value] })),
            },
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
  }, [points, label, t, markers]);

  return <div ref={el} style={{ height }} role="img" aria-label={`${label} chart`} data-testid="area-chart" />;
}
