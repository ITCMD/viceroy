import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Upload } from "lucide-react";
import { useLocation, useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import type { Logo } from "@/components/AppLogo";
import { Button, Card, FormError, PageHeader, Switch, Tabs } from "@/components/ui";
import { useAccountsMutation } from "@/features/accounts/api";
import { HouseholdSettings } from "@/features/household/HouseholdSettings";
import { EmailSettings } from "@/features/email/EmailSettings";
import { MonarchImportDialog } from "@/features/import/MonarchImportDialog";
import { NotificationSettingsCard } from "@/features/notifications/NotificationSettingsCard";
import { api } from "@/lib/api";
import { ApiSettingsCard } from "@/features/api/ApiSettingsCard";
import { AISettingsCard } from "./AISettingsCard";
import { BudgetSettingsCard } from "./BudgetSettingsCard";
import { CategoriesSettings } from "./CategoriesSettings";
import { settingsQuery, type Settings } from "./settings";


function AccountsCard() {
  const qc = useQueryClient();
  const { data } = useQuery(settingsQuery);
  const save = useAccountsMutation(
    (body: Partial<Settings>) => api.patch<Settings>("/settings", body),
    (s) => {
      qc.setQueryData(["settings"], s);
      qc.invalidateQueries({ queryKey: ["transactions"] });
    },
  );
  return (
    <Card title="Accounts">
      <Switch
        label="Paper Cash account"
        hint="A built-in account for cash spending. Turning it off hides it and keeps its history."
        checked={data?.paper_cash_enabled ?? true}
        disabled={!data || save.isPending}
        onCheckedChange={(v) => save.mutate({ paper_cash_enabled: v })}
      />
      <FormError error={save.error} />
    </Card>
  );
}

type SettingsTab = "general" | "household" | "notifications" | "categories";
const tabs: { value: SettingsTab; label: string }[] = [
  { value: "general", label: "General" },
  { value: "household", label: "Household" },
  { value: "notifications", label: "Notifications" },
  { value: "categories", label: "Categories" },
];

export function SettingsPage() {
  const hash = useLocation({ select: (l) => l.hash });
  const asked = useLocation({ select: (l) => (l.search as { tab?: string }).tab });
  const tab = tabs.find((t) => t.value === asked)?.value ?? "general";
  const navigate = useNavigate();
  useEffect(() => {
    if (hash) document.getElementById(hash)?.scrollIntoView({ block: "start" });
  }, [hash]);
  return (
    <>
      <PageHeader title="Settings" />
      <div className="mx-auto flex max-w-4xl flex-col gap-4 p-4 md:p-6">
        <Tabs<SettingsTab>
          value={tab}
          onChange={(t) => navigate({ to: "/settings" as string, search: (t === "general" ? {} : { tab: t }) as never })}
          items={tabs}
        />
        {tab === "categories" ? (
          <CategoriesSettings />
        ) : tab === "household" ? (
          <HouseholdSettings />
        ) : tab === "notifications" ? (
          <NotificationSettingsCard />
        ) : (
          <GeneralSettings />
        )}
      </div>
    </>
  );
}

function GeneralSettings() {
  return (
    <>
      <AccountsCard />
      <AppearanceCard />
      <BudgetSettingsCard />
      <ImportCard />
      <div id="ai" className="scroll-mt-16">
        <AISettingsCard />
      </div>
      <div id="api" className="scroll-mt-16">
        <ApiSettingsCard />
      </div>
      <EmailSettings />
    </>
  );
}

function AppearanceCard() {
  const qc = useQueryClient();
  const { data } = useQuery(settingsQuery);
  const save = useMutation({
    mutationFn: (logo: Logo) => api.patch<Settings>("/settings", { logo }),
    onSuccess: (s) => qc.setQueryData(["settings"], s),
  });
  return (
    <Card title="Appearance">
      <Switch
        label="Butterfly logo"
        hint="Use the viceroy butterfly as the logo and browser icon. Off shows the classic V."
        checked={(data?.logo ?? "butterfly") === "butterfly"}
        disabled={!data || save.isPending}
        onCheckedChange={(v) => save.mutate(v ? "butterfly" : "classic")}
      />
      <FormError error={save.error} />
    </Card>
  );
}

function ImportCard() {
  const [open, setOpen] = useState(false);
  return (
    <Card
      title="Import"
      action={
        <Button size="sm" variant="secondary" onClick={() => setOpen(true)}>
          <Upload size={14} /> Import from Monarch
        </Button>
      }
    >
      <p className="text-sm text-muted">
        Bring in your history from Monarch Money's CSV exports: transactions with their categories, notes and tags, plus account balance history.
      </p>
      <MonarchImportDialog open={open} onOpenChange={setOpen} />
    </Card>
  );
}
