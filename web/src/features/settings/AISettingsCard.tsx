import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, CircleSlash, Mail, Plus } from "lucide-react";
import { useState, type ReactNode } from "react";
import { Badge, Button, Card } from "@/components/ui";
import { chatInfoQuery } from "@/features/chat/api";
import { mailboxesQuery, type EmailAIInfo, type Mailbox } from "@/features/email/api";
import { MailboxDialog } from "@/features/email/MailboxDialog";
import { api } from "@/lib/api";

/** Where AI is set up: the key and models come from viceroy.toml; email reading is per mailbox. */
export function AISettingsCard() {
  const { data: chat } = useQuery(chatInfoQuery);
  const { data: emailAI } = useQuery({ queryKey: ["email", "ai"], queryFn: () => api.get<EmailAIInfo>("/email/ai") });
  const { data: mailboxes } = useQuery(mailboxesQuery);
  const [editing, setEditing] = useState<Mailbox | null>(null);
  const [open, setOpen] = useState(false);
  const edit = (m: Mailbox | null) => {
    setEditing(m);
    setOpen(true);
  };
  const configured = chat?.configured || emailAI?.configured;

  return (
    <Card title="AI">
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <Status ok={chat?.configured} label="Chat with your budget" detail={chat?.configured ? chat.model : "Not set up"} />
          <Status
            ok={emailAI?.configured}
            label="Reading bank emails"
            detail={emailAI?.configured ? `${emailAI.model}${emailAI.local ? " (self-hosted)" : " via OpenRouter"}` : "Not set up"}
          />
        </div>

        {chat && emailAI && !configured && (
          <div className="flex flex-col gap-2 rounded-lg bg-surface-2 px-3 py-2.5 text-[13px]">
            <p>
              AI is set up in <code className="font-mono">viceroy.toml</code> (next to the database), then restart Viceroy. Get a key at openrouter.ai/keys.
            </p>
            <pre className="overflow-x-auto rounded-md bg-surface px-3 py-2 font-mono text-xs" data-testid="ai-config-snippet">
              {`[ai]
openrouter_key = "sk-or-..."
chat_model = "anthropic/claude-sonnet-5.5"
# a cheap model for reading bank emails; empty = chat_model
email_model = ""`}
            </pre>
          </div>
        )}

        <div className="flex flex-col gap-2 border-t border-border pt-4">
          <div>
            <div className="text-[13px] font-medium">Which mailboxes the AI reads</div>
            <p className="text-xs text-muted">
              Only emails no filter caught, and only from the senders you list. It turns payment reminders into due dates on your cards and security
              alerts into notifications.
            </p>
          </div>
          {mailboxes && mailboxes.length === 0 ? (
            <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-dashed border-border px-3 py-2.5 text-[13px] text-muted">
              No mailbox connected yet.
              <Button size="sm" variant="secondary" onClick={() => edit(null)}>
                <Plus size={14} /> Connect a mailbox
              </Button>
            </div>
          ) : (
            <ul className="divide-y divide-border rounded-lg border border-border">
              {(mailboxes ?? []).map((m) => (
                <li key={m.id} className="flex items-center gap-3 px-3 py-2" data-testid="ai-mailbox-row">
                  <Mail size={16} className="shrink-0 text-muted" />
                  <div className="min-w-0 flex-1 text-sm">
                    <div className="truncate font-medium">{m.name || m.username}</div>
                    <div className="truncate text-xs text-muted">{m.ai_read ? senderSummary(m.ai_senders) : "AI reading off"}</div>
                  </div>
                  {m.ai_read && <Badge tone="positive">On</Badge>}
                  <Button size="sm" variant="secondary" onClick={() => edit(m)}>
                    {m.ai_read ? "Edit" : "Turn on"}
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
      <MailboxDialog open={open} onOpenChange={setOpen} mailbox={editing} />
    </Card>
  );
}

function senderSummary(list: string) {
  const senders = list.split("\n").filter(Boolean);
  if (senders.length === 0) return "Every unmatched email";
  return `From ${senders.slice(0, 2).join(", ")}${senders.length > 2 ? ` +${senders.length - 2} more` : ""}`;
}

function Status({ ok, label, detail }: { ok?: boolean; label: string; detail: ReactNode }) {
  const Icon = ok ? CheckCircle2 : CircleSlash;
  return (
    <div className="flex items-center gap-2 text-sm">
      <Icon size={16} className={ok ? "shrink-0 text-positive" : "shrink-0 text-muted"} />
      <span className="font-medium">{label}</span>
      <span className="truncate text-muted">· {detail}</span>
    </div>
  );
}
