import { useQuery } from "@tanstack/react-query";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { BellOff, Check, ChevronRight, CircleDollarSign, Plus, Scale, Sparkles } from "lucide-react";
import { useEffect, useState } from "react";
import { Button, Field, FormError, Select, Sheet, Switch } from "@/components/ui";
import { accountLabel, accountsQuery } from "@/features/accounts/api";
import { AddTransactionDialog } from "@/features/transactions/AddTransactionDialog";
import { api } from "@/lib/api";
import { formatMoney } from "@/lib/format";
import { draftFromMessage, messageQuery, useEmailMutation, type EmailMessage } from "./api";

type ActionResult = { applied: string; filter_id: number | null };

const kindTitles: Record<string, string> = {
  payment_due: "Payment due",
  payment_scheduled: "Payment scheduled",
  payment_received: "Payment received",
  security_alert: "Security alert",
  statement_ready: "Statement ready",
  balance_summary: "Balance update",
  transaction_alert: "Transaction alert",
};

/**
 * Opens the email named by `?email=<id>` (bank notices in the bell and their push
 * notifications link there) in a sheet over the current page: what the bank said, what
 * Viceroy can do about it, and the original email folded away. Closing drops the parameter.
 */
export function EmailNoticeHost() {
  const search = useRouterState({
    select: (s) => s.location.search as Record<string, unknown>,
  });
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const navigate = useNavigate();
  const id = Number(search.email) || 0;
  const { data: m, error } = useQuery({ ...messageQuery(id), enabled: id > 0 });
  const close = () => {
    const { email: _, ...rest } = search;
    navigate({ to: pathname as string, search: rest as never, replace: true });
  };
  const from = m ? m.from_name || m.from_addr : "";
  return (
    <Sheet open={id > 0} onOpenChange={(o) => !o && close()} title={m ? (kindTitles[m.ai_kind] ?? "From your bank") : "From your bank"}>
      <div className="flex flex-col gap-4" data-testid="notice-sheet">
        {!m ? (
          error ? <FormError error={error} /> : <p className="text-sm text-muted">Loading…</p>
        ) : (
          <>
            <div>
              <p className="text-[15px] font-medium">{m.subject || "(no subject)"}</p>
              <p className="mt-0.5 text-[13px] text-muted">
                {from} · {new Date(m.received_at * 1000).toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" })}
              </p>
              {m.ai_summary && (
                <p className="mt-3 flex gap-2 text-sm">
                  <Sparkles size={14} className="mt-0.5 shrink-0 text-accent" aria-label="AI summary" />
                  {m.ai_summary}
                </p>
              )}
            </div>
            <NoticeActions message={m} />
            <details className="group rounded-lg border border-border">
              <summary className="flex cursor-pointer list-none items-center gap-1.5 px-3 py-2.5 text-[13px] font-semibold">
                <ChevronRight size={14} className="text-muted transition group-open:rotate-90" />
                Original email
                <span className="ml-auto truncate pl-2 font-normal text-muted">{m.from_addr}</span>
              </summary>
              <pre className="whitespace-pre-wrap break-words border-t border-border px-3 py-3 font-sans text-[13px]">
                {m.body_text || "The body of this email is no longer stored."}
              </pre>
            </details>
          </>
        )}
      </div>
    </Sheet>
  );
}

/** What Viceroy can do about a notice email: set the balance, mark the bill paid, add a transaction, ignore. */
export function NoticeActions({ message: m }: { message: EmailMessage }) {
  const { data: acctData } = useQuery(accountsQuery);
  const accounts = acctData?.accounts ?? [];
  const facts = m.ai_facts;
  const factsAcct = accounts.find((a) => a.id === facts?.account_id);
  const [always, setAlways] = useState(false);
  const [billAccount, setBillAccount] = useState("");
  const [ignoring, setIgnoring] = useState(false);
  const [subject, setSubject] = useState("");
  const [adding, setAdding] = useState(false);
  const [done, setDone] = useState("");
  const draft = draftFromMessage(m);

  useEffect(() => {
    setAlways(false);
    setIgnoring(false);
    setDone("");
    setSubject(draft.subject_match);
    setBillAccount(String(m.bill?.account_id ?? m.suggested_account_id ?? ""));
  }, [m.id]);

  const act = useEmailMutation(
    (body: Record<string, unknown>) => api.post<ActionResult>(`/email/messages/${m.id}/action`, body),
    (r) => setDone(r.applied),
  );
  const applied = done || m.applied;
  if (m.status === "parsed") return null;

  const balanceOK = facts?.kind === "balance" && !facts.problem && factsAcct;
  const billDue = m.bill?.kind === "due" || m.ai_kind === "payment_due";
  const billAcct = accounts.find((a) => String(a.id) === billAccount);
  const received = new Date(m.received_at * 1000);
  const isoDate = `${received.getFullYear()}-${String(received.getMonth() + 1).padStart(2, "0")}-${String(received.getDate()).padStart(2, "0")}`;

  return (
    <section className="flex flex-col gap-3 rounded-lg border border-border p-3" data-testid="notice-actions">
      <h3 className="text-[13px] font-semibold">{applied ? "Done" : "What do you want to do?"}</h3>
      {applied && (
        <p className="flex items-center gap-2 text-[13px] font-medium text-positive" data-testid="notice-applied">
          <Check size={14} className="shrink-0" /> {applied}
        </p>
      )}
      {!applied && balanceOK && facts && (
        <div className="flex flex-col gap-2">
          <Button
            onClick={() =>
              act.mutate({
                action: "balance",
                always,
                subject_match: draft.subject_match,
              })
            }
            loading={act.isPending && act.variables?.action === "balance"}
          >
            <Scale size={14} /> Update {factsAcct.name} balance to {formatMoney(facts.amount_cents)}
          </Button>
          <p className="text-xs text-muted">
            {facts.as_of
              ? `Balance as of ${new Date(facts.as_of + "T12:00:00").toLocaleDateString(undefined, { month: "short", day: "numeric" })}. `
              : ""}
            {factsAcct.is_manual ? "" : "A later bank sync replaces it only if the bank's balance is newer."}
          </p>
          {facts.can_always ? (
            <Switch
              label="Always do this"
              hint={`Update the balance from every email like this${draft.subject_match ? ` (“${draft.subject_match}”` : " ("}${facts.account_text ? ` mentioning “${facts.account_text}”` : ""}), without AI.`}
              checked={always}
              onCheckedChange={setAlways}
            />
          ) : null}
        </div>
      )}
      {!applied && facts?.problem && (
        <p className="text-[13px] text-muted">The AI saw a balance in this email, but Viceroy couldn't confirm it: {facts.problem}.</p>
      )}
      {!applied && billDue && (
        <div className="flex flex-wrap items-end gap-2">
          {!m.bill?.account_id && (
            <Select
              label="Account"
              className="min-w-40 flex-1"
              value={billAccount}
              onChange={(e) => setBillAccount(e.target.value)}
              options={[
                { value: "", label: "Choose…" },
                ...accounts.map((a) => ({
                  value: String(a.id),
                  label: accountLabel(a),
                })),
              ]}
            />
          )}
          <Button
            variant="secondary"
            disabled={!billAcct}
            loading={act.isPending && act.variables?.action === "bill_paid"}
            onClick={() =>
              act.mutate({
                action: "bill_paid",
                account_id: Number(billAccount),
              })
            }
          >
            <CircleDollarSign size={14} /> Mark {billAcct && m.bill?.account_id ? billAcct.name + " " : ""}bill paid
          </Button>
        </div>
      )}
      {!applied && (
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="secondary" onClick={() => setAdding(true)}>
            <Plus size={14} /> Create transaction
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setIgnoring((v) => !v)} aria-expanded={ignoring}>
            <BellOff size={14} /> Ignore emails like this
          </Button>
        </div>
      )}
      {!applied && ignoring && (
        <div className="flex flex-wrap items-end gap-2">
          <Field
            label="Subject contains"
            className="min-w-48 flex-1"
            value={subject}
            onChange={(e) => setSubject(e.target.value)}
            hint={`Emails from ${m.from_addr} whose subject contains this are ignored from now on.`}
          />
          <Button
            variant="danger"
            disabled={!subject.trim() || !m.subject.toLowerCase().includes(subject.trim().toLowerCase())}
            loading={act.isPending && act.variables?.action === "ignore"}
            onClick={() =>
              act.mutate({
                action: "ignore",
                subject_match: subject.trim(),
                account_id: m.suggested_account_id ?? 0,
              })
            }
          >
            Ignore
          </Button>
        </div>
      )}
      <FormError error={act.error} />
      <AddTransactionDialog
        open={adding}
        onOpenChange={setAdding}
        accounts={accounts}
        defaultAccount={m.bill?.account_id ?? m.suggested_account_id ?? undefined}
        initial={{
          date: m.bill?.date || isoDate,
          description: m.from_name || draft.sender,
          amount: m.bill?.amount_cents ? (m.bill.amount_cents / 100).toFixed(2) : "",
        }}
        onCreated={() => {
          setAdding(false);
          setDone("Created a transaction");
        }}
      />
    </section>
  );
}
