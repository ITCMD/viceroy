import { queryOptions, useQueryClient } from "@tanstack/react-query";
import { useCallback, useRef, useState } from "react";
import { api, ApiError } from "@/lib/api";

export type ChatThread = { id: number; title: string; updated_at: number };

/** What a page's Discuss button hands the chat: a title for the panel and the data on screen
 * (sent as JSON with the first message; money in dollars). */
export type ChatContext = { title: string; page: string; data: unknown; suggestions: string[] };
export type ChatMessage = { id: number; role: "user" | "assistant"; content: string; tools?: string[] };

export const chatInfoQuery = queryOptions({
  queryKey: ["chat"],
  queryFn: () => api.get<{ configured: boolean; model?: string; threads: ChatThread[] }>("/chat"),
});

export const chatThreadQuery = (id: number) =>
  queryOptions({
    queryKey: ["chat", "thread", id],
    queryFn: () => api.get<{ thread: ChatThread; messages: ChatMessage[]; cost: AICost }>(`/chat/threads/${id}`),
  });

/** What AI requests cost: cost_micros in millionths of a dollar; unpriced = requests with no reported cost. */
export type AICost = { requests: number; cost_micros: number; unpriced: number };

export const toolLabels: Record<string, string> = {
  budget_status: "Checked your budget",
  spending_report: "Ran a spending report",
  search_transactions: "Searched transactions",
  net_worth: "Looked at net worth",
  debt_payoff: "Looked at your debts",
  list_accounts: "Looked at your accounts",
  upcoming_recurring: "Checked recurring bills",
  list_goals: "Looked at your goals",
};

type StreamEvent =
  | { type: "thread"; thread: ChatThread }
  | { type: "text"; text: string }
  | { type: "tool"; tool: string }
  | { type: "done" }
  | { type: "error"; error: string };

/** The answer being streamed: text so far and the tools used. `done` = the stream ended; it stays
 * on screen until the saved copy of the conversation includes it. */
export type Pending = { user: string; text: string; tools: string[]; done?: boolean };

/**
 * Sends a message and streams the reply over SSE. `onThread` fires as soon as the server
 * names the thread (new conversations get one on the first message).
 */
export function useChatStream(onThread: (t: ChatThread) => void) {
  const qc = useQueryClient();
  const [pending, setPending] = useState<Pending | null>(null);
  const [error, setError] = useState<string | null>(null);
  const abort = useRef<AbortController | null>(null);

  const send = useCallback(
    async (threadId: number | null, content: string, context?: string) => {
      const ctl = new AbortController();
      abort.current = ctl;
      setError(null);
      let cur: Pending = { user: content, text: "", tools: [] };
      setPending(cur);
      let thread: number | null = threadId;
      try {
        const res = await fetch("/api/chat/messages", {
          method: "POST",
          credentials: "same-origin",
          headers: { "X-Viceroy-CSRF": "1", "Content-Type": "application/json" },
          body: JSON.stringify({ thread_id: threadId ?? 0, content, context: threadId ? undefined : context }),
          signal: ctl.signal,
        });
        if (!res.ok || !res.body) {
          const data = await res.json().catch(() => ({}));
          throw new ApiError(res.status, data.error ?? `Request failed (${res.status})`, data);
        }
        const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
        let buf = "";
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buf += value;
          let cut: number;
          while ((cut = buf.indexOf("\n\n")) >= 0) {
            const chunk = buf.slice(0, cut);
            buf = buf.slice(cut + 2);
            const data = chunk
              .split("\n")
              .filter((l) => l.startsWith("data:"))
              .map((l) => l.slice(5).trim())
              .join("");
            if (!data) continue;
            const e = JSON.parse(data) as StreamEvent;
            if (e.type === "thread") {
              thread = e.thread.id;
              onThread(e.thread);
            } else if (e.type === "text") {
              cur = { ...cur, text: cur.text + e.text };
              setPending(cur);
            } else if (e.type === "tool") {
              cur = { ...cur, tools: [...cur.tools, e.tool] };
              setPending(cur);
            } else if (e.type === "error") {
              setError(e.error);
            }
          }
        }
      } catch (err) {
        if (!ctl.signal.aborted) setError(err instanceof Error ? err.message : String(err));
      } finally {
        abort.current = null;
        // A stopped answer isn't saved, so there's nothing to wait for.
        setPending((p) => (ctl.signal.aborted ? null : p && { ...p, done: true }));
        if (thread) await qc.invalidateQueries({ queryKey: ["chat", "thread", thread] });
        await qc.invalidateQueries({ queryKey: ["chat"], exact: true });
      }
    },
    [qc, onThread],
  );

  const stop = useCallback(() => abort.current?.abort(), []);
  const settle = useCallback(() => setPending(null), []);
  return { send, stop, settle, pending, error, clearError: () => setError(null) };
}
