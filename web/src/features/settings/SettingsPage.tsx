import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ListFilter, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { Button, Card, CategoryPill, EmptyState, FormError, PageHeader } from "@/components/ui";
import { accountsQuery } from "@/features/accounts/api";
import { categoriesQuery, tagsQuery, useTxnMutation } from "@/features/transactions/api";
import { api } from "@/lib/api";
import { RuleDialog } from "./RuleDialog";
import { fieldLabels, opLabels, rulesQuery, type Rule } from "./rules";

export function SettingsPage() {
  return (
    <>
      <PageHeader title="Settings" />
      <div className="mx-auto flex max-w-4xl flex-col gap-4 p-4 md:p-6">
        <RulesCard />
        <p className="text-[13px] text-muted">Notification, household and AI settings arrive in a later phase.</p>
      </div>
    </>
  );
}

function RulesCard() {
  const qc = useQueryClient();
  const [editing, setEditing] = useState<Rule | null>(null);
  const [open, setOpen] = useState(false);
  const { data } = useQuery(rulesQuery);
  const { data: acctData } = useQuery(accountsQuery);
  const { data: cats } = useQuery(categoriesQuery);
  const { data: tagData } = useQuery(tagsQuery);
  const del = useTxnMutation((id: number) => api.del(`/rules/${id}`), () => qc.invalidateQueries({ queryKey: ["rules"] }));
  const rules = data?.rules ?? [];
  const accounts = acctData?.accounts ?? [];
  const allCats = (cats?.groups ?? []).flatMap((g) => g.categories);
  const edit = (r: Rule | null) => {
    setEditing(r);
    setOpen(true);
  };

  return (
    <Card
      title="Rules"
      action={
        <Button size="sm" variant="secondary" onClick={() => edit(null)}>
          <Plus size={14} /> Add rule
        </Button>
      }
    >
      {data && rules.length === 0 ? (
        <EmptyState icon={ListFilter} title="No rules yet">
          Rules categorize, rename, tag or hide transactions automatically when they arrive.
        </EmptyState>
      ) : (
        <ul className="-mx-4 -my-4 divide-y divide-border">
          {rules.map((r) => {
            const cat = allCats.find((c) => c.id === r.set_category_id);
            const acct = accounts.find((a) => a.id === r.account_id);
            const tag = tagData?.tags.find((t) => t.id === r.add_tag_id);
            const actions = [
              cat && <CategoryPill key="c" name={cat.name} icon={cat.icon} />,
              r.set_merchant && <span key="m">rename to “{r.set_merchant}”</span>,
              tag && <span key="t">tag “{tag.name}”</span>,
              r.set_hidden && <span key="h">hide</span>,
            ].filter(Boolean);
            return (
              <li key={r.id} className="flex items-center gap-2 pr-2" data-testid="rule-row">
                <button onClick={() => edit(r)} className="min-w-0 flex-1 px-4 py-3 text-left hover:bg-surface-2">
                  <span className="block truncate text-sm">
                    {fieldLabels[r.match_field]} {opLabels[r.match_op]} <span className="font-medium">“{r.match_value}”</span>
                    {acct && <span className="text-muted"> on {acct.name}</span>}
                  </span>
                  <span className="mt-0.5 flex flex-wrap items-center gap-x-2 text-[13px] text-muted">
                    → {actions.map((a, i) => <span key={i} className="inline-flex items-center">{a}{i < actions.length - 1 && ","}</span>)}
                  </span>
                </button>
                <Button size="sm" variant="danger-ghost" aria-label="Delete rule" loading={del.isPending && del.variables === r.id} onClick={() => del.mutate(r.id)}>
                  <Trash2 size={14} />
                </Button>
              </li>
            );
          })}
        </ul>
      )}
      <FormError error={del.error} />
      <RuleDialog open={open} onOpenChange={setOpen} rule={editing} accounts={accounts} />
    </Card>
  );
}
