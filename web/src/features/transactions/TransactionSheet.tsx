import { useQuery } from "@tanstack/react-query";
import { Landmark, Link2, Mail, PencilLine, Sparkles, Undo2, Unlink, Upload, type LucideIcon } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Badge, Button, CategoryPicker, Field, FormError, MoneyText, Select, Sheet, Switch, TagInput, TextArea } from "@/components/ui";
import { goalsQuery } from "@/features/goals/api";
import { accountLabel } from "@/features/accounts/api";
import { EmailViewer } from "@/features/email/EmailSettings";
import { api } from "@/lib/api";
import {
  categoriesQuery,
  linkCandidatesQuery,
  pendingLabel,
  shortDate,
  similarQuery,
  sourceLabels,
  tagsQuery,
  transactionQuery,
  useTxnMutation,
  type AIChange,
  type Transaction,
  type TxnEmail,
  type TxnSummary,
} from "./api";

const sourceChips: Record<string, { icon: LucideIcon; label: string }> = {
  simplefin: { icon: Landmark, label: "SimpleFIN" },
  email: { icon: Mail, label: "Email alert" },
  manual: { icon: PencilLine, label: "Manual" },
  import: { icon: Upload, label: "Imported" },
};

/** Where this transaction came from: its own source plus any linked pending/email entry. */
function SourceChips({ t, linked }: { t: Transaction; linked: Transaction[] }) {
  const sources = [t.source, ...(t.provisional ? [] : linked.filter((l) => l.provisional).map((l) => l.source))];
  // A manual pending entry isn't a data source worth showing once the bank confirmed it.
  const shown = [...new Set(sources)].filter((s) => !(s === "manual" && t.source !== "manual") && !(s === "manual" && t.provisional));
  return (
    <span className="flex items-center gap-1" data-testid="txn-sources">
      {shown.map((s) => {
        const c = sourceChips[s];
        if (!c) return null;
        const Icon = c.icon;
        return (
          <span key={s} className="inline-flex items-center gap-1 rounded-full bg-surface-2 px-2 py-0.5 text-xs text-muted" title={`From ${c.label}`}>
            <Icon size={12} /> {c.label}
          </span>
        );
      })}
    </span>
  );
}

/** Transaction details: merchant, statement, category, tags, notes, review/hide, links, history. */
export function TransactionSheet({ id, onClose, onSelect }: { id: number | null; onClose: () => void; onSelect: (id: number) => void }) {
  const { data } = useQuery({ ...transactionQuery(id ?? 0), enabled: id !== null });
  const t = data?.transaction;
  return (
    <Sheet open={id !== null} onOpenChange={(o) => !o && onClose()} title={t?.merchant ?? ""}>
      {t && t.id === id && <Details t={t} linked={data.linked} alert={data.email} aiChanges={data.ai_changes ?? []} onClose={onClose} onSelect={onSelect} />}
    </Sheet>
  );
}

function Details({
  t,
  linked,
  alert,
  aiChanges,
  onClose,
  onSelect,
}: {
  t: Transaction;
  linked: Transaction[];
  alert: TxnEmail | null;
  aiChanges: AIChange[];
  onClose: () => void;
  onSelect: (id: number) => void;
}) {
  const [viewingEmail, setViewingEmail] = useState(false);
  const [merchant, setMerchant] = useState(t.merchant);
  const [notes, setNotes] = useState(t.notes);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const { data: cats } = useQuery(categoriesQuery);
  const { data: tagData } = useQuery(tagsQuery);
  const { data: goalData } = useQuery(goalsQuery);
  const goals = (goalData?.goals ?? []).filter((g) => !g.archived || g.id === t.goal_id);
  useEffect(() => {
    setMerchant(t.merchant);
    setNotes(t.notes);
    setConfirmDelete(false);
  }, [t.id, t.merchant, t.notes]);

  const patch = useTxnMutation((body: Record<string, unknown>) => api.patch<Transaction>(`/transactions/${t.id}`, body));
  const unlink = useTxnMutation((provId: number) => api.post(`/transactions/${provId}/unlink`));
  const del = useTxnMutation(() => api.del(`/transactions/${t.id}`), onClose);
  const source = sourceLabels[t.category_source];

  return (
    <div className="flex flex-col gap-6">
      <div>
        <MoneyText cents={t.amount_cents} colored className="text-2xl font-semibold" />
        <div className="mt-1 flex flex-wrap items-center gap-2 text-[13px] text-muted">
          <span>{shortDate(t.date)}</span>
          <span>·</span>
          <span>{accountLabel({ name: t.account_name, mask: t.account_mask })}</span>
          {t.pending && <Badge>{t.source === "email" ? "Pending" : pendingLabel(t)}</Badge>}
          <SourceChips t={t} linked={linked} />
          {t.hidden && <Badge>Hidden</Badge>}
        </div>
      </div>

      <div className="flex flex-col gap-3">
        <form
          className="flex items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            patch.mutate({ merchant });
          }}
        >
          <Field label="Merchant" value={merchant} onChange={(e) => setMerchant(e.target.value)} className="flex-1" />
          {merchant !== t.merchant && (
            <Button size="sm" type="submit" loading={patch.isPending} className="mb-0.5">
              Save
            </Button>
          )}
        </form>
        <div>
          <div className="text-[13px] font-medium">Original statement</div>
          <div className="mt-0.5 break-words font-mono text-xs text-muted" data-testid="original-statement">
            {t.description}
          </div>
          {alert && (
            <button type="button" onClick={() => setViewingEmail(true)} className="mt-1 flex max-w-full items-center gap-1.5 text-xs text-accent hover:underline" data-testid="txn-email">
              <Mail size={12} className="shrink-0" /> <span className="truncate">From email: {alert.subject || alert.from_addr}</span>
            </button>
          )}
          <EmailViewer message={viewingEmail ? alert : null} onClose={() => setViewingEmail(false)} />
        </div>
        <div>
          <CategoryPicker label="Category" groups={cats?.groups ?? []} value={t.category_id} onChange={(v) => patch.mutate({ category_id: v })} />
          {source && <p className="mt-1 text-xs text-muted">{source}</p>}
        </div>
        <TagInput
          label="Tags"
          value={t.tags.map((x) => x.name)}
          onChange={(tags) => patch.mutate({ tags })}
          suggestions={(tagData?.tags ?? []).map((x) => x.name)}
        />
        {goals.length > 0 && (
          <Select
            label="Contribute to goal"
            value={String(t.goal_id ?? "")}
            onChange={(e) => patch.mutate({ goal_id: e.target.value ? Number(e.target.value) : null })}
            options={[{ value: "", label: "None" }, ...goals.map((g) => ({ value: String(g.id), label: `${g.icon} ${g.name}` }))]}
          />
        )}
        <TextArea label="Notes" value={notes} onChange={(e) => setNotes(e.target.value)} onBlur={() => notes !== t.notes && patch.mutate({ notes })} rows={2} />
        <FormError error={patch.error} />
      </div>

      <div className="flex flex-col gap-3 border-t border-border pt-5">
        <Switch label="Needs review" checked={t.needs_review} onCheckedChange={(v) => patch.mutate({ needs_review: v })} />
        <Switch
          label="Hide transaction"
          hint="Leaves it out of budgets and reports."
          checked={t.hidden}
          onCheckedChange={(v) => patch.mutate({ hidden: v })}
        />
      </div>

      {aiChanges.length > 0 && <AIChangesSection t={t} changes={aiChanges} />}
      <LinkSection t={t} linked={linked} unlink={(id) => unlink.mutate(id)} unlinking={unlink.isPending} onSelect={onSelect} />
      <FormError error={unlink.error} />

      {t.merchant_id && <Similar t={t} onSelect={onSelect} />}

      {t.source === "manual" && (
        <div className="flex flex-col gap-2 border-t border-border pt-5">
          <FormError error={del.error} />
          {confirmDelete ? (
            <div className="flex items-center gap-2">
              <span className="text-[13px]">Delete this transaction?</span>
              <Button size="sm" variant="danger" loading={del.isPending} onClick={() => del.mutate(undefined)}>
                Delete
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setConfirmDelete(false)}>
                Cancel
              </Button>
            </div>
          ) : (
            <div>
              <Button size="sm" variant="danger-ghost" onClick={() => setConfirmDelete(true)}>
                Delete transaction
              </Button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function Section({ title, children }: { title: ReactNode; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-2 border-t border-border pt-5">
      <h3 className="text-[13px] font-semibold">{title}</h3>
      {children}
    </div>
  );
}

function SummaryRow({ s, action, onClick }: { s: TxnSummary | Transaction; action?: ReactNode; onClick?: () => void }) {
  const name = "merchant" in s ? s.merchant : s.merchant_name || s.description;
  const sub = [shortDate(s.date), "category_name" in s && s.category_name, "account_name" in s && s.account_name].filter(Boolean).join(" · ");
  const body = (
    <>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm">{name}</span>
        <span className="block truncate text-xs text-muted">{sub}</span>
      </span>
      <MoneyText cents={s.amount_cents} colored className="text-sm" />
    </>
  );
  return (
    <li className="flex items-center gap-3 px-3 py-2">
      {onClick ? (
        <button onClick={onClick} className="flex min-w-0 flex-1 items-center gap-3 text-left hover:opacity-80">
          {body}
        </button>
      ) : (
        body
      )}
      {action}
    </li>
  );
}

function LinkSection({
  t,
  linked,
  unlink,
  unlinking,
  onSelect,
}: {
  t: Transaction;
  linked: Transaction[];
  unlink: (provId: number) => void;
  unlinking: boolean;
  onSelect: (id: number) => void;
}) {
  const [picking, setPicking] = useState(false);
  const candidates = useQuery({ ...linkCandidatesQuery(t.id), enabled: picking });
  const link = useTxnMutation((postedId: number) => api.post(`/transactions/${t.id}/link`, { posted_id: postedId }), () => setPicking(false));

  if (linked.length > 0) {
    // On a posted row, `linked` holds pending entries; on a linked pending entry, the posted row.
    const isPosted = !t.provisional;
    return (
      <Section
        title={
          <span className="flex items-center gap-1.5">
            <Link2 size={14} /> {isPosted ? "Linked pending entry" : "Linked to posted transaction"}
          </span>
        }
      >
        <p className="text-xs text-muted">
          {isPosted
            ? "This transaction replaced a pending entry you added, so it's only counted once."
            : "This entry is replaced by the bank's transaction and no longer counted."}
        </p>
        <ul className="divide-y divide-border rounded-lg border border-border" data-testid="linked-list">
          {linked.map((l) => (
            <SummaryRow
              key={l.id}
              s={l}
              onClick={isPosted ? undefined : () => onSelect(l.id)}
              action={
                <Button size="sm" variant="ghost" loading={unlinking} onClick={() => unlink(isPosted ? l.id : t.id)} aria-label="Unlink">
                  <Unlink size={14} /> Unlink
                </Button>
              }
            />
          ))}
        </ul>
        <p className="text-xs text-muted">Unlinking keeps both and stops them from being linked automatically again.</p>
      </Section>
    );
  }
  if (!t.provisional) return null;
  return (
    <Section title="Link to posted transaction">
      <p className="text-xs text-muted">
        This pending entry links automatically when a matching transaction posts. If it didn't, pick the bank's transaction here.
      </p>
      {picking ? (
        <>
          {candidates.data && candidates.data.transactions.length === 0 && <p className="text-[13px] text-muted">No posted transactions nearby on this account.</p>}
          {candidates.data && candidates.data.transactions.length > 0 && (
            <ul className="divide-y divide-border rounded-lg border border-border">
              {candidates.data.transactions.map((c) => (
                <SummaryRow
                  key={c.id}
                  s={c}
                  action={
                    <Button size="sm" variant="secondary" loading={link.isPending && link.variables === c.id} onClick={() => link.mutate(c.id)}>
                      Link
                    </Button>
                  }
                />
              ))}
            </ul>
          )}
          <FormError error={link.error} />
        </>
      ) : (
        <div>
          <Button size="sm" variant="secondary" onClick={() => setPicking(true)}>
            <Link2 size={14} /> Choose transaction
          </Button>
        </div>
      )}
    </Section>
  );
}

function Similar({ t, onSelect }: { t: Transaction; onSelect: (id: number) => void }) {
  const { data } = useQuery(similarQuery(t.id));
  const list = data?.transactions ?? [];
  if (!data) return null;
  const total = list.reduce((s, x) => s + x.amount_cents, 0);
  return (
    <Section title={`Other transactions at ${t.merchant}`}>
      {list.length === 0 ? (
        <p className="text-[13px] text-muted">None yet.</p>
      ) : (
        <>
          <p className="text-xs text-muted">
            {list.length} {list.length === 1 ? "transaction" : "transactions"} · <MoneyText cents={total} /> total
          </p>
          <ul className="divide-y divide-border rounded-lg border border-border" data-testid="similar-list">
            {list.slice(0, 10).map((s) => (
              <SummaryRow key={s.id} s={s} onClick={() => onSelect(s.id)} />
            ))}
          </ul>
        </>
      )}
    </Section>
  );
}

/** What the email-reading AI changed here, with an undo. */
function AIChangesSection({ t, changes }: { t: Transaction; changes: AIChange[] }) {
  const undo = useTxnMutation(() => api.post<Transaction>(`/transactions/${t.id}/ai-undo`));
  const subject = changes.find((c) => c.email_subject)?.email_subject;
  return (
    <section className="flex flex-col gap-2 rounded-lg border border-border p-3" data-testid="ai-changes">
      <div className="flex items-center justify-between gap-2">
        <h3 className="flex items-center gap-1.5 text-[13px] font-semibold">
          <Sparkles size={14} className="text-accent" /> Changed by AI
        </h3>
        <Button size="sm" variant="secondary" loading={undo.isPending} onClick={() => undo.mutate(undefined)}>
          <Undo2 size={14} /> Undo
        </Button>
      </div>
      {subject && <p className="text-xs text-muted">From the email “{subject}”</p>}
      <ul className="list-disc space-y-0.5 pl-5 text-[13px]">
        {changes.map((c, i) => (
          <li key={i}>{c.description}</li>
        ))}
      </ul>
      <FormError error={undo.error} />
    </section>
  );
}
