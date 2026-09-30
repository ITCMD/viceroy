import * as echarts from "echarts/core";
import { useEffect, useRef } from "react";

/** Mounts an SVG ECharts instance on a div and keeps it sized to the container. */
export function useEChart() {
  const el = useRef<HTMLDivElement>(null);
  const chart = useRef<echarts.ECharts | null>(null);
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
  return { el, chart };
}
