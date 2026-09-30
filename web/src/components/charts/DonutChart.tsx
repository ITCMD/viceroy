import { PieChart } from "echarts/charts";
import { TooltipComponent } from "echarts/components";
import * as echarts from "echarts/core";
import { SVGRenderer } from "echarts/renderers";
import { useEffect } from "react";
import { formatMoney } from "@/lib/format";
import { useChartTokens } from "./tokens";
import { useEChart } from "./useEChart";

echarts.use([PieChart, TooltipComponent, SVGRenderer]);

/** Share-of-total donut (cents). Slices below zero are left out; the caller renders the legend/table. */
export function DonutChart({
  slices,
  label,
  height = 220,
}: {
  slices: { name: string; value: number; color: string }[];
  label: string;
  height?: number;
}) {
  const { el, chart } = useEChart();
  const t = useChartTokens();

  useEffect(() => {
    const data = slices.filter((s) => s.value > 0);
    chart.current?.setOption(
      {
        animationDuration: 300,
        tooltip: {
          trigger: "item",
          backgroundColor: t.surface,
          borderColor: t.border,
          textStyle: { color: t.text, fontSize: 12 },
          formatter: (p: { name: string; value: number; percent: number }) =>
            `${p.name}<br/><b>${formatMoney(p.value)}</b> <span style="color:${t.muted}">${p.percent.toFixed(1)}%</span>`,
        },
        series: [
          {
            type: "pie",
            radius: ["58%", "88%"],
            avoidLabelOverlap: true,
            label: { show: false },
            labelLine: { show: false },
            emphasis: { scale: true, scaleSize: 4 },
            itemStyle: { borderColor: t.surface, borderWidth: 2, borderRadius: 3 },
            data: data.map((s) => ({ name: s.name, value: s.value, itemStyle: { color: s.color } })),
          },
        ],
      },
      true,
    );
  }, [chart, slices, t]);

  const total = slices.reduce((a, s) => a + Math.max(s.value, 0), 0);
  return (
    <div className="relative">
      <div ref={el} style={{ height }} role="img" aria-label={label} />
      {/* Total in the hole, as HTML so it is drawn once and stays crisp. */}
      <div className="pointer-events-none absolute inset-0 grid place-items-center text-center">
        <div>
          <div className="text-xs text-muted">Total</div>
          <div className="text-lg font-semibold tabular">{formatMoney(total, { whole: true })}</div>
        </div>
      </div>
    </div>
  );
}
