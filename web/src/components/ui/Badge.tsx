import clsx from "clsx";
import type { ReactNode } from "react";

type Tone = "neutral" | "accent" | "warning" | "negative" | "positive";

const tones: Record<Tone, string> = {
  neutral: "bg-surface-2 text-muted",
  accent: "bg-accent-soft text-accent",
  warning: "bg-accent-soft text-accent",
  negative: "bg-negative/10 text-negative",
  positive: "bg-positive/10 text-positive",
};

/** Small status pill. Always carries a text label, never color alone. */
export function Badge({ tone = "neutral", children }: { tone?: Tone; children: ReactNode }) {
  return (
    <span className={clsx("inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium", tones[tone])}>
      {children}
    </span>
  );
}
