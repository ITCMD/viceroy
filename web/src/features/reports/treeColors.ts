import type { ChartTokens } from "@/components/charts/tokens";
import { shade } from "@/lib/color";
import type { TreeNode } from "./api";

/** One color per spending section, the same in the flow diagram and the spending map. */
export function sectionColor(n: TreeNode, t: ChartTokens, index: number) {
  if (n.kind === "contributions") return t.series[2];
  if (n.kind === "uncategorized") return t.muted;
  const byKind: Record<string, string> = { fixed: t.series[0], flexible: t.series[1], non_monthly: t.series[6] };
  return byKind[n.group] ?? t.series[(3 + index) % t.series.length];
}

/** Children of a box get shades of its color so related boxes read as a family. */
export function childColor(parent: string, k: number) {
  const steps = [0, 0.16, -0.14, 0.3, -0.26, 0.42];
  return shade(parent, steps[k % steps.length]);
}
