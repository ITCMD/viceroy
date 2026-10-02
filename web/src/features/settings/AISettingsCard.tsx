import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, CircleSlash, Mail, Plus } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Badge, Button, Card, Field, FormError } from "@/components/ui";
import { mailboxesQuery, type Mailbox } from "@/features/email/api";
import { MailboxDialog } from "@/features/email/MailboxDialog";
import { api } from "@/lib/api";
import { aiSettingsQuery, modelsQuery, type AISettings } from "./ai";
import { ModelPicker } from "./ModelPicker";

type Patch = Partial<{ openrouter_key: string; chat_model: string; email_model: string; email_base_url: string; vision_model: string }>;
type Target = "chat" | "email" | "vision";
type TestResult = { ok: boolean; model?: string; error?: string };


/** OpenRouter key and models (admins edit; saved settings win over viceroy.toml), plus which mailboxes the AI reads. */
export function AISettingsCard() {
  const qc = useQueryClient();
  const { data: s } = useQuery(aiSettingsQuery);
  const { data: mailboxes } = useQuery(mailboxesQuery);
  const models = useQuery(modelsQuery("openrouter", !!s));
  const localModels = useQuery(modelsQuery("email", !!s?.email_base_url));
  const [editing, setEditing] = useState<Mailbox | null>(null);
  const [turnOn, setTurnOn] = useState(false);
  const [open, setOpen] = useState(false);
  const [replacing, setReplacing] = useState(false);
  const [key, setKey] = useState("");
  const [chatModel, setChatModel] = useState("");
  const [emailModel, setEmailModel] = useState("");
  const [baseURL, setBaseURL] = useState("");
  const [visionModel, setVisionModel] = useState("");
  useEffect(() => {
    if (!s) return;
    setChatModel(s.chat_model);
    setEmailModel(s.email_model);
    setBaseURL(s.email_base_url);
    setVisionModel(s.vision_model);
  }, [s]);

  const save = useMutation({
    mutationFn: (p: Patch) => api.patch<AISettings>("/settings/ai", p),
    onSuccess: (next) => {
      qc.setQueryData(aiSettingsQuery.queryKey, next);
      qc.invalidateQueries({ queryKey: ["chat"] });
      qc.invalidateQueries({ queryKey: ["email", "ai"] });
      setKey("");
      setReplacing(false);
      test.reset();
    },
  });
  const test = useMutation({ mutationFn: (target: Target) => api.post<TestResult>("/settings/ai/test", { target }) });
  const edit = (m: Mailbox | null, turnOnAI = false) => {
    setEditing(m);
    setTurnOn(turnOnAI);
    setOpen(true);
  };
  const canEdit = !!s?.can_edit;
  const dirty = !!s && (chatModel !== s.chat_model || emailModel !== s.email_model || baseURL !== s.email_base_url || visionModel !== s.vision_model);
  const showKeyInput = !!s && (!s.key_set || replacing);

  return (
    <Card title="AI">
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <Status ok={s?.chat_ready} label="Chat with your budget" detail={s?.chat_ready ? s.chat_model : "Needs an API key"} />
          <Status
            ok={s?.email_ready}
            label="Reading bank emails"
            detail={s?.email_ready ? `${s.email_model || s.chat_model}${s.email_base_url ? " (self-hosted)" : " via OpenRouter"}` : "Needs an API key"}
          />
          <Status
            ok={s?.vision_ready}
            label="Reading budgets & screenshots"
            detail={s?.vision_ready ? s.vision_model || s.chat_model : "Needs an API key"}
          />
        </div>

        <div className="flex flex-col gap-3 border-t border-border pt-4">
          {s && !canEdit && <p className="text-[13px] text-muted">Only an admin can change these.</p>}
          {s?.key_set && !replacing && (
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="text-[13px]">
                <div className="font-medium">OpenRouter API key</div>
                <div className="text-muted" data-testid="ai-key-status">
                  Saved key ending in {s.key_hint || "…"}
                  {s.key_source === "config" && " (from viceroy.toml)"}
                </div>
              </div>
              {canEdit && (
                <div className="flex gap-1">
                  <Button size="sm" variant="secondary" onClick={() => setReplacing(true)}>
                    Replace
                  </Button>
                  {s.key_source === "settings" && (
                    <Button size="sm" variant="danger-ghost" loading={save.isPending && save.variables?.openrouter_key === ""} onClick={() => save.mutate({ openrouter_key: "" })}>
                      Remove
                    </Button>
                  )}
                </div>
              )}
            </div>
          )}
          {showKeyInput && (
            <form
              className="flex flex-wrap items-end gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                if (key.trim()) save.mutate({ openrouter_key: key });
              }}
            >
              <Field
                label="OpenRouter API key"
                type="password"
                autoComplete="off"
                value={key}
                onChange={(e) => setKey(e.target.value)}
                placeholder="sk-or-v1-…"
                hint="Create one at openrouter.ai/keys. It's stored encrypted and never shown again."
                disabled={!canEdit}
                className="min-w-60 flex-1"
              />
              <Button type="submit" className="mb-5" disabled={!canEdit || !key.trim()} loading={save.isPending && !!save.variables?.openrouter_key}>
                Save key
              </Button>
              {replacing && (
                <Button type="button" variant="ghost" className="mb-5" onClick={() => setReplacing(false)}>
                  Cancel
                </Button>
              )}
            </form>
          )}

          <div className="grid gap-3 sm:grid-cols-2">
            <ModelPicker
              label="Chat model"
              value={chatModel}
              onChange={setChatModel}
              models={models.data?.models}
              error={models.error?.message}
              need="tools"
              emptyLabel={s?.config_chat_model ? `Default (${s.config_chat_model})` : undefined}
              hint="Needs tool calling."
              disabled={!canEdit}
            />
            <ModelPicker
              label="Email reading model"
              value={emailModel}
              onChange={setEmailModel}
              models={(s?.email_base_url ? localModels : models).data?.models}
              error={(s?.email_base_url ? localModels : models).error?.message}
              emptyLabel="Same as chat model"
              hint={s?.email_base_url ? "Models served by your self-hosted endpoint." : "A cheap, fast model is plenty (e.g. a DeepSeek flash model)."}
              disabled={!canEdit}
            />
            <ModelPicker
              label="Multimodal model"
              value={visionModel}
              onChange={setVisionModel}
              models={models.data?.models}
              error={models.error?.message}
              need="images"
              emptyLabel="Same as chat model"
              hint="Reads budgets you paste or screenshot (Budget → Import). Pick a stronger model that accepts images."
              disabled={!canEdit}
            />
          </div>
          <details className="text-[13px]" open={!!s?.email_base_url}>
            <summary className="cursor-pointer text-muted">Read emails with a self-hosted model instead</summary>
            <Field
              label="Self-hosted endpoint (OpenAI-compatible)"
              value={baseURL}
              onChange={(e) => setBaseURL(e.target.value)}
              placeholder="http://127.0.0.1:11434/v1"
              hint="For example Ollama. Emails then never leave your server; set the email reading model to a model it serves."
              disabled={!canEdit}
              className="mt-2"
            />
          </details>
          <div className="flex flex-wrap items-center gap-2">
            {dirty && (
              <Button size="sm" loading={save.isPending && !save.variables?.openrouter_key} onClick={() => save.mutate({ chat_model: chatModel, email_model: emailModel, email_base_url: baseURL, vision_model: visionModel })}>
                Save
              </Button>
            )}
            {canEdit && (
              <>
                <Button size="sm" variant="secondary" disabled={!s?.chat_ready || dirty} loading={test.isPending && test.variables === "chat"} onClick={() => test.mutate("chat")}>
                  Test chat
                </Button>
                <Button size="sm" variant="secondary" disabled={!s?.email_ready || dirty} loading={test.isPending && test.variables === "email"} onClick={() => test.mutate("email")}>
                  Test email reading
                </Button>
                <Button size="sm" variant="secondary" disabled={!s?.vision_ready || dirty} loading={test.isPending && test.variables === "vision"} onClick={() => test.mutate("vision")}>
                  Test images
                </Button>
              </>
            )}
            {test.data && (
              <span className={test.data.ok ? "text-[13px] text-positive" : "text-[13px] text-negative"} role="status">
                {test.data.ok ? `Works (${test.data.model}).` : test.data.error}
              </span>
            )}
          </div>
          <FormError error={save.error ?? test.error} />
        </div>

        <div className="flex flex-col gap-2 border-t border-border pt-4">
          <div>
            <div className="text-[13px] font-medium">Which mailboxes the AI reads</div>
            <p className="text-xs text-muted">
              Only emails no filter caught, and only from the senders you list. It turns payment reminders into due dates on your cards and security
              alerts into notifications, and can annotate the purchase an email is about.
            </p>
            <details className="text-xs text-muted" data-testid="ai-sandbox">
              <summary className="cursor-pointer font-medium text-text">What the AI can and can't do</summary>
              <ul className="mt-1.5 list-disc space-y-0.5 pl-5">
                <li>It reads one email at a time, marked as untrusted text it must not take instructions from.</li>
                <li>It can look up your categories, rules, schedule (recurring bills, card payments) and transactions within 14 days of the email.</li>
                <li>It can change at most 3 of those transactions per email: set the category (never one you chose), add a short note (links are removed) and tags.</li>
                <li>It can't change amounts, dates, accounts or merchants, delete anything, see other emails, or reach your settings or keys.</li>
                <li>Every change is flagged for review and listed on the transaction with an Undo button.</li>
                <li>
                  For a purchase, withdrawal or deposit email no filter caught, it can propose a filter. Viceroy only saves it after checking it: the
                  amount and merchant must be in the email, the account is picked by its last 4 digits (exactly one must match), and the filter must
                  read the same values back. Its transactions are marked for review, and it never changes a filter you made.
                </li>
              </ul>
            </details>
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
                  <Button size="sm" variant="secondary" onClick={() => edit(m, !m.ai_read && !!s?.email_ready)}>
                    {m.ai_read ? "Edit" : "Turn on"}
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
      <MailboxDialog open={open} onOpenChange={setOpen} mailbox={editing} turnOnAI={turnOn} />
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
