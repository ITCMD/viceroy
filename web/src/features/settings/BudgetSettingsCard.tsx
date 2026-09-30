import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Button, Card, Field, FormError, Select, Switch } from "@/components/ui";
import { useBudgetMutation, weekdays, type BudgetSettings, type PaySchedule } from "@/features/budget/api";
import { api } from "@/lib/api";
import { settingsQuery, type Settings } from "./settings";

const payKinds: { value: PaySchedule["kind"]; label: string }[] = [
  { value: "weekly", label: "Every week" },
  { value: "biweekly", label: "Every two weeks" },
  { value: "semimonthly", label: "Twice a month" },
  { value: "monthly", label: "Once a month" },
];

function defaultsFor(kind: PaySchedule["kind"]): PaySchedule {
  const today = new Date().toISOString().slice(0, 10);
  if (kind === "weekly" || kind === "biweekly") return { kind, anchor: today };
  return kind === "semimonthly" ? { kind, days: [1, 15] } : { kind, days: [1] };
}

export function BudgetSettingsCard() {
  const qc = useQueryClient();
  const { data } = useQuery(settingsQuery);
  const save = useBudgetMutation(
    (budget: Partial<BudgetSettings>) => api.patch<Settings>("/settings", { budget }),
    (s) => qc.setQueryData(["settings"], s),
  );
  const saved = data?.budget.pay_schedule;
  const [pay, setPay] = useState<PaySchedule | null>(null);
  useEffect(() => {
    if (saved) setPay(saved);
  }, [saved]);
  const dirty = !!pay && JSON.stringify(pay) !== JSON.stringify(saved);
  const setDay = (i: number, v: string) => pay && setPay({ ...pay, days: (pay.days ?? []).map((d, j) => (j === i ? Number(v) : d)) });

  return (
    <Card title="Budget">
      <div className="flex flex-col gap-4">
        <Switch
          label="Apply budget changes to future months"
          hint="The default for the toggle in the budget editor."
          checked={data?.budget.forward_default ?? false}
          disabled={!data || save.isPending}
          onCheckedChange={(v) => save.mutate({ forward_default: v })}
        />
        <Select
          label="Weeks start on"
          value={String(data?.budget.week_start ?? 0)}
          disabled={!data}
          onChange={(e) => save.mutate({ week_start: Number(e.target.value) })}
          options={weekdays.map((d, i) => ({ value: String(i), label: d }))}
          className="max-w-60"
        />
        {pay && (
          <div className="flex flex-col gap-3 border-t border-border pt-4">
            <div>
              <div className="text-[13px] font-medium">Pay schedule</div>
              <p className="text-xs text-muted">The paycheck view budgets from one payday to the next.</p>
            </div>
            <div className="flex flex-wrap items-end gap-3">
              <Select
                label="Paid"
                value={pay.kind}
                onChange={(e) => setPay(defaultsFor(e.target.value as PaySchedule["kind"]))}
                options={payKinds}
                className="w-44"
              />
              {(pay.kind === "weekly" || pay.kind === "biweekly") && (
                <Field label="A recent payday" type="date" value={pay.anchor ?? ""} onChange={(e) => setPay({ ...pay, anchor: e.target.value })} />
              )}
              {(pay.kind === "semimonthly" || pay.kind === "monthly") &&
                (pay.days ?? []).map((d, i) => (
                  <Field
                    key={i}
                    label={pay.kind === "monthly" ? "Day of month" : i === 0 ? "First payday" : "Second payday"}
                    type="number"
                    min={1}
                    max={31}
                    value={d}
                    onChange={(e) => setDay(i, e.target.value)}
                    className="w-28"
                  />
                ))}
              {dirty && (
                <Button size="sm" className="mb-0.5" loading={save.isPending} onClick={() => save.mutate({ pay_schedule: pay })}>
                  Save
                </Button>
              )}
            </div>
          </div>
        )}
        <FormError error={save.error} />
      </div>
    </Card>
  );
}
