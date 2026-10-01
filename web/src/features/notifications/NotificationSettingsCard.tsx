import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BellRing, Smartphone, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Button, Card, Field, FormError, Switch } from "@/components/ui";
import { api } from "@/lib/api";
import { timeAgo } from "@/lib/format";
import { deviceName, notifySettingsQuery, type Device, type NotifyPrefs, type NotifySettings } from "./api";
import { currentEndpoint, enablePush, pushUnavailableReason, removeDevice } from "./push";

const unavailableText: Record<string, string> = {
  https:
    "Push notifications need HTTPS. Set public_url in viceroy.toml and open Viceroy through it (for example behind a reverse proxy). Alerts still show under the bell.",
  unsupported: "This browser doesn't support push notifications. On iPhone, add Viceroy to the Home Screen first.",
  denied: "Notifications are blocked for this site. Allow them in the browser's site settings, then reload.",
};

export function NotificationSettingsCard() {
  const qc = useQueryClient();
  const { data } = useQuery(notifySettingsQuery);
  const setData = (s: NotifySettings) => qc.setQueryData(notifySettingsQuery.queryKey, s);
  const save = useMutation({
    mutationFn: (p: NotifyPrefs) => api.put<NotifySettings>("/notifications/settings", p),
    onSuccess: setData,
  });
  const prefs = data?.prefs;
  const update = (patch: Partial<NotifyPrefs>) => prefs && save.mutate({ ...prefs, ...patch });

  // Threshold inputs are edited locally and saved with a button, like the pay schedule.
  const [pct, setPct] = useState("");
  const [amount, setAmount] = useState("");
  useEffect(() => {
    if (!prefs) return;
    setPct(String(prefs.pacing_pct));
    setAmount(String(Math.floor(prefs.large_txn_cents / 100)));
  }, [prefs]);
  const pctOK = /^\d+$/.test(pct);
  const amountOK = /^\d+$/.test(amount);
  const dirty =
    !!prefs && ((pctOK && Number(pct) !== prefs.pacing_pct) || (amountOK && Number(amount) * 100 !== prefs.large_txn_cents));

  return (
    <Card title="Notifications">
      <div className="flex flex-col gap-4">
        <DevicesSection settings={data} />
        <div className="flex flex-col gap-4 border-t border-border pt-4">
          <div>
            <div className="text-[13px] font-medium">Alert me when</div>
            <p className="text-xs text-muted">Each alert fires once per category per month, or once per transaction.</p>
          </div>
          <Switch
            label="A category goes over budget"
            checked={prefs?.over_budget ?? true}
            disabled={!prefs || save.isPending}
            onCheckedChange={(v) => update({ over_budget: v })}
          />
          <Switch
            label="Spending runs ahead of pace"
            hint="Compared with what the category's spending timing expects by today."
            checked={prefs?.pacing ?? true}
            disabled={!prefs || save.isPending}
            onCheckedChange={(v) => update({ pacing: v })}
          />
          <Switch
            label="A large transaction posts"
            hint="Money out from a synced account or an email alert. Entries you add yourself are skipped."
            checked={prefs?.large_txn ?? true}
            disabled={!prefs || save.isPending}
            onCheckedChange={(v) => update({ large_txn: v })}
          />
          <Switch
            label="A card payment is due soon"
            hint="3 days before a due date read from a bank email, unless a payment is scheduled."
            checked={prefs?.payment_due ?? true}
            disabled={!prefs || save.isPending}
            onCheckedChange={(v) => update({ payment_due: v })}
          />
          <Switch
            label="My bank sends something important"
            hint="Security alerts and other notices the AI finds in bank emails (turn on AI reading per mailbox under Email alerts)."
            checked={prefs?.bank_notices ?? true}
            disabled={!prefs || save.isPending}
            onCheckedChange={(v) => update({ bank_notices: v })}
          />
          <Switch
            label="An account stops syncing"
            checked={prefs?.disconnected ?? true}
            disabled={!prefs || save.isPending}
            onCheckedChange={(v) => update({ disconnected: v })}
          />
          <div className="flex flex-wrap items-end gap-3">
            <Field
              label="Ahead of pace by (%)"
              inputMode="numeric"
              value={pct}
              onChange={(e) => setPct(e.target.value.trim())}
              disabled={!prefs?.pacing}
              className="w-40"
            />
            <Field
              label="Large transaction ($)"
              inputMode="numeric"
              value={amount}
              onChange={(e) => setAmount(e.target.value.replace(/[$,\s]/g, ""))}
              disabled={!prefs?.large_txn}
              className="w-40"
            />
            {dirty && (
              <Button
                size="sm"
                className="mb-0.5"
                loading={save.isPending}
                disabled={!pctOK || !amountOK}
                onClick={() => update({ pacing_pct: Number(pct), large_txn_cents: Number(amount) * 100 })}
              >
                Save
              </Button>
            )}
          </div>
        </div>
        <FormError error={save.error} />
      </div>
    </Card>
  );
}

function DevicesSection({ settings }: { settings?: NotifySettings }) {
  const qc = useQueryClient();
  const refresh = () => qc.invalidateQueries({ queryKey: notifySettingsQuery.queryKey });
  const [here, setHere] = useState<string | null>(null);
  const [unavailable, setUnavailable] = useState(pushUnavailableReason);
  useEffect(() => {
    currentEndpoint().then(setHere);
  }, [settings]);

  const enable = useMutation({
    mutationFn: () => enablePush(settings!.public_key),
    onSuccess: (d) => {
      setHere(d.endpoint);
      refresh();
    },
    onSettled: () => setUnavailable(pushUnavailableReason()),
  });
  const remove = useMutation({ mutationFn: removeDevice, onSettled: refresh });
  const test = useMutation({
    mutationFn: () => api.post<{ sent: number }>("/notifications/test"),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notifications"], exact: true }),
  });
  const devices = settings?.devices ?? [];
  const thisOn = !!here && devices.some((d) => d.endpoint === here);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="text-[13px]">
          <div className="font-medium">Push notifications</div>
          <p className="text-xs text-muted">
            {thisOn ? "On for this device." : "Get alerts on this device even when Viceroy is closed."}
          </p>
        </div>
        <div className="flex gap-2">
          <Button size="sm" variant="secondary" loading={test.isPending} onClick={() => test.mutate()}>
            <BellRing size={14} /> Send test
          </Button>
          {!thisOn && !unavailable && (
            <Button size="sm" loading={enable.isPending} disabled={!settings} onClick={() => enable.mutate()}>
              Enable on this device
            </Button>
          )}
        </div>
      </div>
      {unavailable && !thisOn && <p className="rounded-lg bg-surface-2 px-3 py-2 text-[13px] text-muted">{unavailableText[unavailable]}</p>}
      {test.data && (
        <p className="text-[13px] text-muted" role="status">
          {test.data.sent > 0
            ? `Sent to ${test.data.sent} device${test.data.sent === 1 ? "" : "s"}. It's also under the bell.`
            : "Added under the bell. No devices have push turned on yet."}
        </p>
      )}
      {devices.length > 0 && (
        <ul className="divide-y divide-border rounded-lg border border-border">
          {devices.map((d: Device) => (
            <li key={d.id} className="flex items-center gap-3 px-3 py-2" data-testid="push-device">
              <Smartphone size={16} className="shrink-0 text-muted" />
              <div className="min-w-0 flex-1 text-sm">
                <span className="font-medium">{deviceName(d.user_agent)}</span>
                {d.endpoint === here && <span className="text-muted"> · this device</span>}
                <span className="block text-xs text-muted">Added {timeAgo(d.created_at)}</span>
              </div>
              <Button
                size="sm"
                variant="danger-ghost"
                aria-label="Remove device"
                loading={remove.isPending && remove.variables?.id === d.id}
                onClick={() => remove.mutate(d)}
              >
                <Trash2 size={14} />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <FormError error={enable.error ?? remove.error ?? test.error} />
    </div>
  );
}
