import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { ArrowUp, Check, History, MessageSquarePlus, Sparkles, Square, Trash2 } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { Popover } from "radix-ui";
import { useCallback, useEffect, useRef, useState } from "react";
import { EmptyState, Markdown, Sheet } from "@/components/ui";
import { api } from "@/lib/api";
import { timeAgo } from "@/lib/format";
import { chatInfoQuery, chatThreadQuery, toolLabels, useChatStream, type ChatMessage, type ChatThread } from "./api";

const suggestions = [
  "How am I doing on my budget this month?",
  "What did I spend the most on last month?",
  "What bills are coming up in the next two weeks?",
  "How has my net worth changed this year?",
];

const headerButton = "grid size-7 place-items-center rounded-md text-muted hover:bg-surface-2 hover:text-text";

/** "Chat with your budget": a side panel with the conversation and a composer. */
export function ChatSheet({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { data: info } = useQuery({ ...chatInfoQuery, enabled: open });
  const [threadId, setThreadId] = useState<number | null>(null);
  const onThread = useCallback((t: ChatThread) => setThreadId(t.id), []);
  const { send, stop, settle, pending, error, clearError } = useChatStream(onThread);
  const { data: thread } = useQuery({ ...chatThreadQuery(threadId ?? 0), enabled: open && !!threadId });
  const [draft, setDraft] = useState("");
  const scroller = useRef<HTMLDivElement>(null);

  const messages: ChatMessage[] = thread && thread.thread.id === threadId ? thread.messages : [];
  // While streaming, the server copy of the new question may already be loaded; show it once.
  const shown =
    pending && messages.at(-1)?.role === "user" && messages.at(-1)?.content === pending.user ? messages.slice(0, -1) : messages;
  // A finished answer stays local until the saved conversation has it (the refetch can lag).
  const saved = !!pending?.done && messages.at(-1)?.role === "assistant" && messages.at(-1)?.content === pending.text;
  useEffect(() => {
    if (pending?.done && (saved || error || !pending.text)) settle();
  }, [pending?.done, pending?.text, saved, error, settle]);

  useEffect(() => {
    const el = scroller.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [shown.length, pending?.text, pending?.tools.length, error]);

  const submit = (text: string) => {
    const t = text.trim();
    if (!t || (pending && !pending.done)) return;
    setDraft("");
    send(threadId, t);
  };
  const newChat = () => {
    if (pending && !pending.done) return;
    settle();
    setThreadId(null);
    clearError();
  };

  const actions = info?.configured && (
    <>
      <ThreadPicker current={threadId} threads={info.threads} onPick={(id) => !(pending && !pending.done) && (settle(), setThreadId(id), clearError())} onDeleted={(id) => id === threadId && newChat()} />
      <button className={headerButton} aria-label="New chat" title="New chat" onClick={newChat} disabled={!!pending && !pending.done}>
        <MessageSquarePlus size={16} />
      </button>
    </>
  );

  return (
    <Sheet open={open} onOpenChange={onOpenChange} title="Chat with your budget" actions={actions} className="!max-w-xl" bodyClassName="!p-0 flex flex-col">
      {info && !info.configured ? (
        <EmptyState icon={Sparkles} title="Chat isn't set up">
          Add an OpenRouter API key in{" "}
          <Link to={"/settings" as string} hash="ai" className="font-medium text-accent hover:underline" onClick={() => onOpenChange(false)}>
            Settings → AI
          </Link>
          .
        </EmptyState>
      ) : (
        <>
          <div ref={scroller} className="flex-1 overflow-y-auto px-5 py-4" data-testid="chat-messages">
            {shown.length === 0 && !pending ? (
              <div className="flex flex-col gap-4 pt-6">
                <div className="flex flex-col items-center gap-2 text-center">
                  <div className="grid size-11 place-items-center rounded-full bg-accent-soft text-accent">
                    <Sparkles size={20} />
                  </div>
                  <h3 className="text-[15px] font-semibold">Ask about your money</h3>
                  <p className="max-w-sm text-sm text-muted">
                    Answers come from your budget, transactions, accounts and goals. Chat can read your data but never changes it.
                  </p>
                </div>
                <div className="grid gap-2 sm:grid-cols-2">
                  {suggestions.map((s) => (
                    <button key={s} onClick={() => submit(s)} className="rounded-lg border border-border px-3 py-2 text-left text-[13px] hover:bg-surface-2">
                      {s}
                    </button>
                  ))}
                </div>
              </div>
            ) : (
              <div className="flex flex-col gap-4">
                {shown.map((m) => (
                  <Bubble key={m.id} role={m.role} text={m.content} tools={m.tools} />
                ))}
                {pending && (
                  <>
                    <Bubble role="user" text={pending.user} />
                    <Bubble role="assistant" text={pending.text} tools={pending.tools} streaming />
                  </>
                )}
              </div>
            )}
            {error && (
              <p className="mt-4 rounded-lg bg-negative/10 px-3 py-2 text-[13px] text-negative" role="alert">
                {error}
              </p>
            )}
          </div>
          <form
            className="flex items-end gap-2 border-t border-border p-3"
            onSubmit={(e) => {
              e.preventDefault();
              submit(draft);
            }}
          >
            <textarea
              aria-label="Message"
              rows={1}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  submit(draft);
                }
              }}
              placeholder="Ask about your budget…"
              className="max-h-40 min-h-9 flex-1 resize-none rounded-lg border border-border bg-surface px-3 py-2 text-sm outline-none transition [field-sizing:content] placeholder:text-muted focus:border-accent focus:ring-2 focus:ring-accent/20"
            />
            {pending && !pending.done ? (
              <button type="button" onClick={stop} aria-label="Stop" className="grid size-9 shrink-0 place-items-center rounded-lg bg-surface-2 text-text hover:brightness-95">
                <Square size={14} fill="currentColor" />
              </button>
            ) : (
              <button
                type="submit"
                aria-label="Send"
                disabled={!draft.trim()}
                className="grid size-9 shrink-0 place-items-center rounded-lg bg-accent text-accent-fg transition hover:brightness-95 disabled:opacity-50"
              >
                <ArrowUp size={16} />
              </button>
            )}
          </form>
          {info?.model && <p className="-mt-1 px-4 pb-2 text-[11px] text-muted">Answers by {info.model} via OpenRouter. Check important numbers.</p>}
        </>
      )}
    </Sheet>
  );
}

function Bubble({ role, text, tools, streaming }: { role: "user" | "assistant"; text: string; tools?: string[]; streaming?: boolean }) {
  if (role === "user") {
    return (
      <div className="ml-10 self-end whitespace-pre-wrap rounded-2xl rounded-br-md bg-accent-soft px-3.5 py-2 text-sm" data-testid="chat-user">
        {text}
      </div>
    );
  }
  const used = [...new Set(tools ?? [])];
  return (
    <div className="mr-6 flex flex-col gap-1.5" data-testid="chat-assistant">
      {used.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {used.map((t) => (
            <span key={t} className="inline-flex items-center gap-1 rounded-full bg-surface-2 px-2 py-0.5 text-xs text-muted">
              <Check size={12} /> {toolLabels[t] ?? t}
            </span>
          ))}
        </div>
      )}
      {text ? (
        <Markdown text={text} />
      ) : (
        streaming && (
          <span className="inline-flex gap-1 py-2" aria-label="Thinking">
            {[0, 1, 2].map((i) => (
              <span key={i} className="size-1.5 animate-pulse rounded-full bg-muted" style={{ animationDelay: `${i * 150}ms` }} />
            ))}
          </span>
        )
      )}
    </div>
  );
}

function ThreadPicker({
  current,
  threads,
  onPick,
  onDeleted,
}: {
  current: number | null;
  threads: ChatThread[];
  onPick: (id: number) => void;
  onDeleted: (id: number) => void;
}) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const del = useMutation({
    mutationFn: (id: number) => api.del(`/chat/threads/${id}`),
    onSuccess: (_, id) => {
      onDeleted(id);
      qc.invalidateQueries({ queryKey: ["chat"], exact: true });
    },
  });
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger className={headerButton} aria-label="Past chats" title="Past chats">
        <History size={16} />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content align="end" sideOffset={6} collisionPadding={12} className="z-50 w-72 rounded-xl border border-border bg-surface p-1 shadow-xl outline-none">
          {threads.length === 0 ? (
            <p className="px-3 py-6 text-center text-[13px] text-muted">No past chats yet.</p>
          ) : (
            <ul className="max-h-80 overflow-y-auto">
              {threads.map((t) => (
                <li key={t.id} data-testid="chat-thread" className={clsx("group flex items-center rounded-lg hover:bg-surface-2", t.id === current && "bg-surface-2")}>
                  <button
                    className="min-w-0 flex-1 px-3 py-2 text-left"
                    onClick={() => {
                      onPick(t.id);
                      setOpen(false);
                    }}
                  >
                    <span className="block truncate text-sm">{t.title || "Untitled chat"}</span>
                    <span className="block text-xs text-muted">{timeAgo(t.updated_at)}</span>
                  </button>
                  <button
                    className="mr-1 rounded-md p-1.5 text-muted hover:text-negative focus:opacity-100 md:opacity-0 md:group-hover:opacity-100"
                    aria-label={`Delete “${t.title}”`}
                    onClick={() => del.mutate(t.id)}
                  >
                    <Trash2 size={14} />
                  </button>
                </li>
              ))}
            </ul>
          )}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
