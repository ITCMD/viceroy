import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type Security = "tls" | "starttls" | "none";

export type Mailbox = {
  id: number;
  name: string;
  host: string;
  port: number;
  security: Security;
  username: string;
  folder: string;
  enabled: boolean;
  status: "new" | "ok" | "error";
  last_error: string;
  last_checked_at: number | null;
  ai_read: boolean;
  ai_senders: string;
};

export type EmailAIInfo = { configured: boolean; model?: string; local?: boolean };

export type FieldSpec = { before?: string; after?: string; regex?: string };
export type CustomParser = { amount: FieldSpec; merchant: FieldSpec; date: FieldSpec };

export type EmailFilter = {
  id: number;
  name: string;
  priority: number;
  enabled: boolean;
  sender: string;
  subject_match: string;
  body_match: string;
  use_regex: boolean;
  account_id: number;
  parser: string;
  custom_parser: CustomParser | null;
  sign: "debit" | "credit";
  /** "ai": written by the email-reading AI and checked by Viceroy; editing makes it "user". */
  source: "user" | "ai";
};

export type Template = { name: string; label: string };

export type MessageStatus = "unrouted" | "parsed" | "parse_failed" | "ignored" | "noticed";

export type EmailMessageRow = {
  id: number;
  from_addr: string;
  from_name: string;
  subject: string;
  received_at: number;
  status: MessageStatus;
  filter_id: number | null;
  filter_name: string;
  transaction_id: number | null;
  error: string;
  ai_status: "" | "pending" | "done" | "failed";
  ai_kind: string;
  ai_summary: string;
  /** Why no filter could be built from the AI's reading of a transaction email. */
  ai_problem?: string;
};

/** What the AI read from a transaction email, and Viceroy's verdict on it. */
export type RecipeCheck = {
  recipe: {
    direction: "out" | "in" | string;
    amount: string;
    merchant: string;
    date: string;
    account_text: string;
    subject_contains: string;
    amount_rule: FieldSpec;
    merchant_rule: FieldSpec;
    date_rule: FieldSpec | null;
  };
  account_id?: number;
  problem?: string;
  sender?: string;
  subject_match?: string;
  body_match?: string;
  parser?: string;
  custom_parser?: string;
  sign?: "debit" | "credit";
};

export type EmailMessage = Omit<EmailMessageRow, "filter_name" | "ai_problem"> & {
  body_text: string;
  ai_recipe: RecipeCheck | null;
  suggested_account_id: number | null;
  account_phrase: string;
  suggested_sign: "debit" | "credit";
};

export type Parsed = { amount_cents: number; merchant: string; date: string };

export type PreviewMatch = {
  id: number;
  subject: string;
  from_addr: string;
  received_at: number;
  status: MessageStatus;
  parsed: Parsed | null;
  error: string;
  /** The draft filter's conditions catch this email. */
  matches: boolean;
};

export type Preview = { filter_error: string; parser_error: string; matches: PreviewMatch[]; sample: PreviewMatch | null };

export const mailboxesQuery = queryOptions({
  queryKey: ["email", "mailboxes"],
  queryFn: () => api.get<Mailbox[]>("/email/mailboxes"),
  // Status changes in the background (first check, errors).
  refetchInterval: (q) => (q.state.data?.some((m) => m.enabled && m.status === "new") ? 2000 : 30000),
});

export const filtersQuery = queryOptions({
  queryKey: ["email", "filters"],
  queryFn: () => api.get<EmailFilter[]>("/email/filters"),
});

export const templatesQuery = queryOptions({
  queryKey: ["email", "templates"],
  queryFn: () => api.get<Template[]>("/email/templates"),
  staleTime: Infinity,
});

export const openMessagesQuery = queryOptions({
  queryKey: ["email", "messages", "open"],
  queryFn: () =>
    api.get<{ messages: EmailMessageRow[]; counts: { unrouted: number; parse_failed: number } }>("/email/messages?status=open"),
  refetchInterval: 30000,
});

export const messageQuery = (id: number) =>
  queryOptions({ queryKey: ["email", "message", id], queryFn: () => api.get<EmailMessage>(`/email/messages/${id}`) });

/** Mutations touching email also refresh transactions, since routing an alert creates one. */
export function useEmailMutation<TVars, TRes = unknown>(fn: (v: TVars) => Promise<TRes>, onSuccess?: (r: TRes) => void) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess,
    onSettled: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: ["email"] }),
        qc.invalidateQueries({ queryKey: ["transactions"] }),
        qc.invalidateQueries({ queryKey: ["accounts"] }),
      ]),
  });
}

export const statusLabels: Record<MessageStatus, string> = {
  unrouted: "No filter",
  parsed: "Imported",
  parse_failed: "Couldn't read",
  ignored: "Ignored",
  noticed: "Read by AI",
};

/** One line about what the AI made of an unmatched email, or "" when it hasn't been read. */
export function aiNote(m: Pick<EmailMessageRow, "ai_status" | "ai_kind" | "ai_summary" | "ai_problem">) {
  switch (m.ai_status) {
    case "pending":
      return "Waiting for AI…";
    case "failed":
      return m.ai_summary || "AI couldn't read it.";
    case "done":
      if (m.ai_kind === "transaction_alert")
        return m.ai_problem
          ? `AI: looks like a transaction, but couldn't set up a filter on its own (${m.ai_problem}). Create one to import these.`
          : `AI: looks like a purchase alert. Create a filter to import these. ${m.ai_summary}`.trim();
      if (m.ai_kind === "ignore") return "AI: nothing to act on.";
      return m.ai_summary ? `AI: ${m.ai_summary}` : "";
  }
  return "";
}

const notMerchants = new Set(["us", "you", "your", "me", "the", "a", "an", "this", "it", "our", "any", "your account", "your card"]);

/** Mirrors the server's check: a value that can't be a merchant name ("us", "a", digits only). */
export const plausibleMerchant = (s: string) => s.trim().length >= 2 && !notMerchants.has(s.trim().toLowerCase()) && /[\p{L}]/u.test(s);

/** A starting filter for an unrouted email: its sender's domain and the subject's lead words. */
export function draftFromMessage(m: { from_addr: string; subject: string }) {
  const domain = m.from_addr.split("@")[1] ?? m.from_addr;
  // Stop before the first amount or digit so the snippet matches other alerts of the same kind.
  const lead = m.subject.split(/[$\d]/)[0].trim().split(/\s+/).slice(0, 4).join(" ");
  return { sender: domain, subject_match: lead.length >= 4 ? lead : "" };
}
