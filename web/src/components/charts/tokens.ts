import { useEffect, useState } from "react";

/** Reads design tokens from CSS so charts follow light/dark mode. */
export function readTokens() {
  const cs = getComputedStyle(document.documentElement);
  const v = (n: string) => cs.getPropertyValue(n).trim();
  return {
    text: v("--c-text"),
    muted: v("--c-muted"),
    border: v("--c-border"),
    surface: v("--c-surface"),
    accent: v("--c-accent"),
    positive: v("--c-positive"),
    negative: v("--c-negative"),
    /** Categorical series colors, in fixed order. */
    series: [1, 2, 3, 4, 5, 6, 7, 8].map((i) => v(`--c-chart-${i}`)),
  };
}

export type ChartTokens = ReturnType<typeof readTokens>;

export function useChartTokens() {
  const [tokens, setTokens] = useState(readTokens);
  useEffect(() => {
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => setTokens(readTokens());
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);
  return tokens;
}
