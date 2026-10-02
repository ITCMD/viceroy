import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { useMemo, useState } from "react";
import { AreaChart } from "@/components/charts/AreaChart";
import { MessageSquarePlus } from "lucide-react";
import { Button, Card, MoneyText, Segmented } from "@/components/ui";
import { netWorthQuery } from "./api";
import { NetWorthNoteDialog, netWorthNotesQuery, type NoteDraft } from "./NetWorthNotes";

const ranges = [
  { days: 30, label: "1M" },
  { days: 90, label: "3M" },
  { days: 365, label: "1Y" },
  { days: 1825, label: "5Y" },
];

/** Net worth headline, change over the range and history chart (Accounts tab and Dashboard). */
export function NetWorthCard() {
  const [days, setDays] = useState(90);
  const { data } = useQuery(netWorthQuery(days));
  const points = useMemo(() => (data?.points ?? []).map((p) => ({ date: p.date, value: p.net })), [data]);
  const last = points.at(-1)?.value ?? 0;
  const first = points[0]?.value ?? 0;
  const change = last - first;
  const label = ranges.find((r) => r.days === days)!.label;
  const { data: notes } = useQuery(netWorthNotesQuery);
  const markers = useMemo(() => (notes?.annotations ?? []).map((n) => ({ id: n.id, date: n.date, label: n.label, icon: n.icon })), [notes]);
  const [draft, setDraft] = useState<NoteDraft | null>(null);

  return (
    <Card>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="text-[13px] font-medium text-muted">Net worth</div>
          <MoneyText cents={last} className="text-3xl font-semibold tracking-tight" />
          <div className={clsx("mt-0.5 text-[13px] tabular", change > 0 ? "text-positive" : change < 0 ? "text-negative" : "text-muted")}>
            {change >= 0 ? "+" : "−"}
            <MoneyText cents={Math.abs(change)} /> <span className="text-muted">over {label}</span>
          </div>
        </div>
        <div className="flex items-center gap-1">
          <Button
            size="sm"
            variant="ghost"
            aria-label="Add a note to the chart"
            title="Add a note (or right-click a day on the chart)"
            onClick={() => setDraft({ date: points.at(-1)?.date ?? new Date().toLocaleDateString("en-CA") })}
          >
            <MessageSquarePlus size={15} />
          </Button>
          <Segmented label="Time range" value={days} onChange={setDays} items={ranges.map((r) => ({ value: r.days, label: r.label }))} />
        </div>
      </div>
      <div className="mt-3">
        <AreaChart
          points={points}
          label="Net worth"
          markers={markers}
          onPickDate={(date) => setDraft({ date })}
          onMarkerClick={(id) => {
            const note = notes?.annotations.find((n) => n.id === id);
            if (note) setDraft({ note });
          }}
        />
      </div>
      <NetWorthNoteDialog draft={draft} onClose={() => setDraft(null)} />
    </Card>
  );
}
