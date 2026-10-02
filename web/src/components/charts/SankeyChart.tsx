import { SankeyChart as ESankey } from "echarts/charts";
import { TooltipComponent } from "echarts/components";
import * as echarts from "echarts/core";
import { SVGRenderer } from "echarts/renderers";
import { useEffect, useRef } from "react";
import { formatMoney } from "@/lib/format";
import { useChartTokens } from "./tokens";
import { useEChart } from "./useEChart";

echarts.use([ESankey, TooltipComponent, SVGRenderer]);

export type FlowNode = {
  key: string;
  label: string;
  /** Second label line, e.g. "$1,234 · 12%". */
  sub: string;
  color: string;
  depth: number;
  clickable?: boolean;
};
export type FlowLink = { source: string; target: string; value: number };

/**
 * Money flowing between columns of boxes (cents), left to right: wide rounded nodes,
 * flows shaded from their source color to their target color. Labels sit right of a node,
 * except in the last column where they sit left so they stay inside the chart.
 */
export function SankeyChart({
  nodes,
  links,
  label,
  height,
  onNodeClick,
}: {
  nodes: FlowNode[];
  links: FlowLink[];
  label: string;
  height: number;
  onNodeClick?: (key: string) => void;
}) {
  const { el, chart } = useEChart();
  const t = useChartTokens();
  const click = useRef(onNodeClick);
  click.current = onNodeClick;

  useEffect(() => {
    const c = chart.current;
    if (!c) return;
    const byKey = new Map(nodes.map((n) => [n.key, n]));
    const lastDepth = Math.max(0, ...nodes.map((n) => n.depth));
    c.setOption(
      {
        animationDuration: 400,
        tooltip: {
          trigger: "item",
          backgroundColor: t.surface,
          borderColor: t.border,
          textStyle: { color: t.text, fontSize: 12 },
          confine: true,
          formatter: (p: { dataType: string; name: string; data: { source?: string; target?: string; value: number } }) => {
            if (p.dataType === "edge") {
              const s = byKey.get(p.data.source!)?.label ?? "";
              const d = byKey.get(p.data.target!)?.label ?? "";
              return `${s} → ${d}<br/><b>${formatMoney(p.data.value)}</b>`;
            }
            const n = byKey.get(p.name);
            return n ? `${n.label}<br/><b>${n.sub}</b>` : "";
          },
        },
        series: [
          {
            type: "sankey",
            left: 4,
            right: 4,
            top: 8,
            bottom: 8,
            nodeWidth: 14,
            nodeGap: 26, // room for two-line labels on thin boxes
            nodeAlign: "left",
            layoutIterations: 0, // keep our order: largest first in every column
            draggable: false,
            emphasis: { focus: "adjacency" },
            data: nodes.map((n) => ({
              name: n.key,
              depth: n.depth,
              itemStyle: { color: n.color, borderRadius: 4, borderWidth: 0 },
              label: {
                position: n.depth === lastDepth && lastDepth > 0 ? "left" : "right",
                formatter: () => `{name|${n.label}}\n{sub|${n.sub}}`,
              },
              cursor: n.clickable ? "pointer" : "default",
            })),
            links: links.map((l) => ({ ...l, lineStyle: { color: "gradient", opacity: 0.28 } })),
            lineStyle: { curveness: 0.55 },
            label: {
              color: t.text,
              distance: 6,
              rich: {
                name: { fontSize: 12, color: t.text, lineHeight: 15 },
                sub: { fontSize: 11, color: t.muted, lineHeight: 14 },
              },
            },
          },
        ],
      },
      true,
    );
  }, [chart, nodes, links, t]);

  useEffect(() => {
    const c = chart.current;
    if (!c) return;
    const h = (p: { dataType?: string; name: string }) => {
      if (p.dataType === "node") click.current?.(p.name);
    };
    c.on("click", h);
    return () => {
      c.off("click", h);
    };
  }, [chart]);

  return <div ref={el} style={{ height }} role="img" aria-label={label} />;
}
