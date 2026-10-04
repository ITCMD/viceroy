import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { ChevronRight, Sparkles } from "lucide-react";
import { useMemo, useState } from "react";
import { Glyph } from "@/components/ui";
import { monthLabel } from "@/features/budget/api";
import { ChatSheet } from "@/features/chat/ChatSheet";
import type { ChatContext } from "@/features/chat/api";
import { formatMoney } from "@/lib/format";
import { riskLabel, riskQuery, riskTone, type Risk, type RiskDriver, type RiskLevel } from "./api";

/** Half-circle gauge for a 0-100 risk score. */
export function RiskGauge({ score, level, size = 112 }: { score: number; level: RiskLevel; size?: number }) {
  const arc = "M 10 60 A 50 50 0 0 1 110 60";
  return (
    <svg viewBox="0 0 120 68" width={size} height={(size * 68) / 120} role="img" aria-label={`Risk score ${score} of 100, ${riskLabel[level]}`}>
      <path d={arc} pathLength={100} fill="none" strokeWidth={11} strokeLinecap="round" className="stroke-surface-2" />
      {score > 0 && (
        <path
          d={arc}
          pathLength={100}
          fill="none"
          strokeWidth={11}
          strokeLinecap="round"
          strokeDasharray={`${Math.max(score, 2)} 100`}
          className={riskTone[level].stroke}
        />
      )}
      <text x={60} y={58} textAnchor="middle" className="fill-text text-[22px] font-semibold tabular">
        {score}
      </text>
    </svg>
  );
}

const reasonLabel: Record<RiskDriver["reason"], string> = {
  over: "already over",
  upcoming: "bills still to come",
  pace: "ahead of pace",
};

const dollars = (c: number) => c / 100;

/** What the chat sees when asked to explain the risk (money in dollars). */
function riskContext(r: Risk): ChatContext {
  const level = riskLabel[r.level].toLowerCase();
  return {
    title: "Risk of overspending",
    page: "Dashboard: Risk of overspending meter",
    ask:
      r.level === "low"
        ? `My risk of overspending in ${monthLabel(r.month)} is low. Is there anything I should keep an eye on?`
        : `Why is my risk of overspending ${level} this month, and what can I do about it?`,
    suggestions: [
      "Which category should I cut back on first?",
      "Can I move money between categories to cover this?",
      "How did I do on these categories last month?",
    ],
    data: {
      how_it_works:
        "Score 0-100 for ending the month over budget. Each expense category (not income, goals, or categories hidden from the budget) is projected to month end as: spent so far + the rest of its plan after this week (spending ahead of the plan through pace_through carries through), or at least spent + recurring bills still to come. projected_over sums each category's projected overspending; 15% of the budget scores 100.",
      month: r.month,
      today: r.today,
      pace_through: r.pace_through,
      score: r.score,
      level: r.level,
      budget: dollars(r.budget),
      spent: dollars(r.actual),
      projected: dollars(r.projected),
      projected_over: dollars(r.projected_over),
      categories_counted: r.counted,
      drivers: r.drivers.map((d) => ({
        category: d.name,
        why: reasonLabel[d.reason],
        budget: dollars(d.budget),
        spent: dollars(d.actual),
        planned_through_this_week: dollars(d.planned),
        recurring_still_to_come: dollars(d.upcoming),
        projected: dollars(d.projected),
        projected_over: dollars(d.over),
      })),
    },
  };
}

/** Dashboard meter: how likely this month is to end over budget, from weekly pacing. Opens a
 * chat that explains it. */
export function RiskCard() {
  const { data: r } = useQuery(riskQuery);
  const [chatting, setChatting] = useState(false);
  const context = useMemo(() => (r ? riskContext(r) : undefined), [r]);
  if (!r) return null;
  const tone = riskTone[r.level];
  const top = r.drivers.slice(0, 3);
  const summary =
    r.projected_over > 0
      ? `On pace to end about ${formatMoney(r.projected_over, { whole: true })} over in ${r.drivers.length} ${r.drivers.length === 1 ? "category" : "categories"}.`
      : r.counted > 0
        ? "Spending is on track with this week's plan."
        : "Set a budget to see your risk of overspending.";
  return (
    <>
      <button
        type="button"
        onClick={() => setChatting(true)}
        className="group flex w-full flex-col gap-3 rounded-xl border border-border bg-surface p-4 text-left transition hover:bg-surface-2/50 sm:flex-row sm:items-center sm:gap-5"
        data-testid="risk-card"
      >
        <div className="flex items-center gap-4">
          <RiskGauge score={r.score} level={r.level} />
          <div className="min-w-0 flex-1 sm:w-56 sm:flex-none">
            <div className="text-[13px] font-medium text-muted">Risk of overspending · {monthLabel(r.month).split(" ")[0]}</div>
            <div className={clsx("text-xl font-semibold tracking-tight", tone.text)} data-testid="risk-level">
              {riskLabel[r.level]}
            </div>
            <p className="mt-0.5 text-[13px] text-muted">{summary}</p>
          </div>
        </div>
        {top.length > 0 && (
          <ul className="flex min-w-0 flex-1 flex-col gap-1.5 border-t border-border pt-3 sm:border-l sm:border-t-0 sm:pl-5 sm:pt-0">
            {top.map((d) => (
              <li key={d.id} className="flex items-center gap-2 text-[13px]">
                <Glyph icon={d.icon} className="shrink-0" />
                <span className="min-w-0 flex-1 truncate font-medium">{d.name}</span>
                <span className="hidden text-muted sm:inline">{d.reason === "over" ? `${formatMoney(d.actual - d.budget, { whole: true })} over already` : reasonLabel[d.reason]}</span>
                <span className="tabular text-negative" title="Projected over by month end">
                  +{formatMoney(d.over, { whole: true })}
                </span>
              </li>
            ))}
          </ul>
        )}
        <span className="flex items-center gap-1 self-end text-[13px] font-medium text-accent sm:self-center">
          <Sparkles size={14} />
          {r.level === "low" ? "Ask AI" : "Why?"}
          <ChevronRight size={14} className="transition group-hover:translate-x-0.5" />
        </span>
      </button>
      <ChatSheet open={chatting} onOpenChange={setChatting} context={context} />
    </>
  );
}
