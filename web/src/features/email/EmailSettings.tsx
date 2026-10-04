import { useQuery } from "@tanstack/react-query";
import { Inbox, Mail, Plus, RefreshCw, Sparkles, Trash2 } from "lucide-react";
import { useState } from "react";
import { Badge, Button, Card, Dialog, EmptyState, FormError } from "@/components/ui";
import { accountLabel, accountsQuery } from "@/features/accounts/api";
import { api } from "@/lib/api";
import { timeAgo } from "@/lib/format";
import {
  aiNote,
  filtersQuery,
  mailboxesQuery,
  messageQuery,
  openMessagesQuery,
  statusLabels,
  templatesQuery,
  useEmailMutation,
  type EmailFilter,
  type EmailMessageRow,
  type Mailbox,
} from "./api";
import { FilterDialog } from "./FilterDialog";
import { MailboxDialog } from "./MailboxDialog";

/** Settings → Email alerts tab: mailboxes, filters, and emails that still need a filter. */
export function EmailSettings() {
  const { data: acctData } = useQuery(accountsQuery);
  const accounts = acctData?.accounts ?? [];
  const [filterOpen, setFilterOpen] = useState(false);
  const [editingFilter, setEditingFilter] = useState<EmailFilter | null>(null);
  const [fromMessage, setFromMessage] = useState<EmailMessageRow | null>(null);
  const openFilter = (f: EmailFilter | null, m: EmailMessageRow | null = null) => {
    setEditingFilter(f);
    setFromMessage(m);
    setFilterOpen(true);
  };

  return (
    <>
      <MailboxesCard />
      <div id="email-filters" className="scroll-mt-16">
        <FiltersCard onEdit={(f) => openFilter(f)} accounts={accounts} />
      </div>
      <ReviewEmailsCard onCreateFilter={(m) => openFilter(null, m)} />
      <FilterDialog open={filterOpen} onOpenChange={setFilterOpen} filter={editingFilter} fromMessage={fromMessage} accounts={accounts} />
    </>
  );
}

function mailboxStatus(m: Mailbox) {
  if (!m.enabled) return <Badge>Paused</Badge>;
  if (m.status === "error") return <Badge tone="negative">Error</Badge>;
  if (m.status === "new") return <Badge tone="accent">Connecting…</Badge>;
  return <Badge tone="positive">Watching</Badge>;
}

function MailboxesCard() {
  const { data } = useQuery(mailboxesQuery);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Mailbox | null>(null);
  const check = useEmailMutation((id: number) => api.post(`/email/mailboxes/${id}/check`));
  const del = useEmailMutation((id: number) => api.del(`/email/mailboxes/${id}`));
  const edit = (m: Mailbox | null) => {
    setEditing(m);
    setOpen(true);
  };
  const boxes = data ?? [];

  return (
    <Card
      title="Mailboxes"
      action={
        <Button size="sm" variant="secondary" onClick={() => edit(null)}>
          <Plus size={14} /> Connect mailbox
        </Button>
      }
    >
      {data && boxes.length === 0 ? (
        <EmptyState icon={Mail} title="Get transactions in minutes">
          Turn on transaction alerts at your bank, then connect the mailbox they arrive in. Alerts show up long before the daily bank sync.
        </EmptyState>
      ) : (
        <ul className="-mx-4 -my-4 divide-y divide-border">
          {boxes.map((m) => (
            <li key={m.id} className="flex items-center gap-2 pr-2" data-testid="mailbox-row">
              <button onClick={() => edit(m)} className="min-w-0 flex-1 px-4 py-3 text-left hover:bg-surface-2">
                <span className="flex items-center gap-2 text-sm">
                  <span className="truncate font-medium">{m.name}</span>
                  {mailboxStatus(m)}
                </span>
                <span className="mt-0.5 block truncate text-[13px] text-muted">
                  {m.folder} on {m.host}
                  {m.last_checked_at && ` · checked ${timeAgo(m.last_checked_at)}`}
                </span>
                {m.status === "error" && m.last_error && <span className="mt-0.5 block text-[13px] text-negative">{m.last_error}</span>}
              </button>
              <Button size="sm" variant="ghost" aria-label="Check now" loading={check.isPending && check.variables === m.id} onClick={() => check.mutate(m.id)}>
                <RefreshCw size={14} />
              </Button>
              <Button size="sm" variant="danger-ghost" aria-label="Remove mailbox" loading={del.isPending && del.variables === m.id} onClick={() => del.mutate(m.id)}>
                <Trash2 size={14} />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <FormError error={check.error ?? del.error} />
      <MailboxDialog open={open} onOpenChange={setOpen} mailbox={editing} />
    </Card>
  );
}

function FiltersCard({ onEdit, accounts }: { onEdit: (f: EmailFilter | null) => void; accounts: { id: number; name: string; mask: string }[] }) {
  const { data } = useQuery(filtersQuery);
  const { data: templates } = useQuery(templatesQuery);
  const del = useEmailMutation((id: number) => api.del(`/email/filters/${id}`));
  const filters = data ?? [];
  const conditions = (f: EmailFilter) =>
    [f.sender && `from ${f.sender}`, f.subject_match && `subject “${f.subject_match}”`, f.body_match && `body “${f.body_match}”`]
      .filter(Boolean)
      .join(", ");

  return (
    <Card
      title="Email filters"
      action={
        <Button size="sm" variant="secondary" onClick={() => onEdit(null)}>
          <Plus size={14} /> Add filter
        </Button>
      }
    >
      {data && filters.length === 0 ? (
        <p className="text-[13px] text-muted">
          Filters send each bank's alerts to the right account. Create one from an email below, or add one by hand.
        </p>
      ) : (
        <ul className="-mx-4 -my-4 divide-y divide-border">
          {filters.map((f) => {
            const acct = accounts.find((a) => a.id === f.account_id);
            const parser = f.parser === "custom" ? "custom parser" : (templates?.find((t) => t.name === f.parser)?.label ?? f.parser);
            return (
              <li key={f.id} className="flex items-center gap-2 pr-2" data-testid="email-filter-row">
                <button onClick={() => onEdit(f)} className="min-w-0 flex-1 px-4 py-3 text-left hover:bg-surface-2">
                  <span className="flex items-center gap-2 text-sm">
                    <span className="truncate font-medium">{f.name}</span>
                    {!f.enabled && <Badge>Off</Badge>}
                    {f.source === "ai" && (
                      <Badge tone="accent">
                        <Sparkles size={10} /> Made by AI
                      </Badge>
                    )}
                  </span>
                  <span className="mt-0.5 block truncate text-[13px] text-muted">
                    {f.action === "ignore" ? (
                      <>{conditions(f)} → ignored</>
                    ) : (
                      <>
                        {conditions(f)} → {acct ? accountLabel(acct) : "missing account"}
                        {f.action === "balance" ? " · updates the balance" : ` · ${parser}`}
                        {f.action === "transaction" && f.sign === "credit" && " · deposits"}
                      </>
                    )}
                  </span>
                </button>
                <Button size="sm" variant="danger-ghost" aria-label="Delete filter" loading={del.isPending && del.variables === f.id} onClick={() => del.mutate(f.id)}>
                  <Trash2 size={14} />
                </Button>
              </li>
            );
          })}
        </ul>
      )}
      <FormError error={del.error} />
    </Card>
  );
}

function ReviewEmailsCard({ onCreateFilter }: { onCreateFilter: (m: EmailMessageRow) => void }) {
  const { data: boxes } = useQuery(mailboxesQuery);
  const { data } = useQuery({ ...openMessagesQuery, enabled: (boxes?.length ?? 0) > 0 });
  const [viewing, setViewing] = useState<EmailMessageRow | null>(null);
  const ignore = useEmailMutation((id: number) => api.post(`/email/messages/${id}/ignore`));
  if (!boxes?.length) return null;
  const msgs = data?.messages ?? [];

  return (
    <Card title={`Emails to review${msgs.length ? ` (${msgs.length})` : ""}`}>
      {data && msgs.length === 0 ? (
        <EmptyState icon={Inbox} title="All caught up">
          Emails that match no filter, or that a filter couldn't read, show up here.
        </EmptyState>
      ) : (
        <ul className="-mx-4 -my-4 divide-y divide-border" id="emails-to-review">
          {msgs.map((m) => (
            <li key={m.id} className="flex flex-wrap items-center gap-2 px-4 py-3 md:flex-nowrap" data-testid="review-email-row">
              <button onClick={() => setViewing(m)} className="min-w-0 flex-1 text-left">
                <span className="flex items-center gap-2 text-sm">
                  <span className="truncate font-medium">{m.subject || "(no subject)"}</span>
                  <Badge tone={m.status === "parse_failed" ? "negative" : "neutral"}>{statusLabels[m.status]}</Badge>
                </span>
                <span className="mt-0.5 block truncate text-[13px] text-muted">
                  {m.from_name ? `${m.from_name} <${m.from_addr}>` : m.from_addr} · {timeAgo(m.received_at)}
                  {m.status === "parse_failed" && ` · ${m.filter_name}: ${m.error}`}
                </span>
                {aiNote(m) && (
                  <span className="mt-0.5 flex items-center gap-1 text-[13px] text-accent" data-testid="email-ai-note">
                    <Sparkles size={12} className="shrink-0" /> <span className="truncate">{aiNote(m)}</span>
                  </span>
                )}
              </button>
              <div className="flex gap-1">
                <Button size="sm" variant="secondary" onClick={() => onCreateFilter(m)}>
                  {m.status === "parse_failed" ? "Fix filter" : "Create filter"}
                </Button>
                <Button size="sm" variant="ghost" loading={ignore.isPending && ignore.variables === m.id} onClick={() => ignore.mutate(m.id)}>
                  Ignore
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
      <FormError error={ignore.error} />
      <EmailViewer message={viewing} onClose={() => setViewing(null)} onCreateFilter={onCreateFilter} />
    </Card>
  );
}

/** Shows an email as plain text (HTML is never rendered). */
export function EmailViewer({
  message,
  onClose,
  onCreateFilter,
}: {
  message: { id: number; from_addr: string; subject: string } | null;
  onClose: () => void;
  onCreateFilter?: (m: EmailMessageRow) => void;
}) {
  const { data } = useQuery({ ...messageQuery(message?.id ?? 0), enabled: !!message });
  return (
    <Dialog
      open={!!message}
      onOpenChange={(o) => !o && onClose()}
      className="md:max-w-2xl"
      title={message?.subject || data?.subject || "(no subject)"}
      description={data ? `${data.from_name ? `${data.from_name} <${data.from_addr}>` : data.from_addr} · ${new Date(data.received_at * 1000).toLocaleString()}` : message?.from_addr}
      footer={
        onCreateFilter && data && data.status !== "parsed" ? (
          <Button
            size="sm"
            onClick={() => {
              onClose();
              onCreateFilter({ ...data, filter_name: "" });
            }}
          >
            Create filter
          </Button>
        ) : undefined
      }
    >
      <pre className="whitespace-pre-wrap break-words font-sans text-[13px]">
        {data ? data.body_text || "The body of this email is no longer stored." : "Loading…"}
      </pre>
    </Dialog>
  );
}
