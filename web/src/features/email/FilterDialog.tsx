import { keepPreviousData, useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { useEffect, useMemo, useState } from "react";
import { Badge, Button, Dialog, Field, FormError, MoneyText, Segmented, Select, Switch } from "@/components/ui";
import { accountLabel, type Account } from "@/features/accounts/api";
import { api } from "@/lib/api";
import {
  draftFromMessage,
  messageQuery,
  statusLabels,
  templatesQuery,
  useEmailMutation,
  type CustomParser,
  type EmailFilter,
  type FieldSpec,
  type Parsed,
  type Preview,
  type PreviewMatch,
} from "./api";

const emptyParser: CustomParser = { amount: { before: "Amount" }, merchant: { before: "Merchant" }, date: {} };

function useDebounced<T>(value: T, ms: number) {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

/**
 * Create or edit an email filter: which alerts it matches, the account they go to, and how
 * the amount/merchant/date are read. A live preview runs the draft against recent emails.
 */
export function FilterDialog({
  open,
  onOpenChange,
  filter,
  fromMessage,
  accounts,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  filter: EmailFilter | null;
  fromMessage: { id: number; from_addr: string; subject: string } | null;
  accounts: Account[];
}) {
  const { data: templates } = useQuery(templatesQuery);
  const [name, setName] = useState("");
  const [sender, setSender] = useState("");
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const [regex, setRegex] = useState(false);
  const [accountId, setAccountId] = useState("");
  const [sign, setSign] = useState<EmailFilter["sign"]>("debit");
  const [parser, setParser] = useState("generic");
  const [custom, setCustom] = useState<CustomParser>(emptyParser);
  const [enabled, setEnabled] = useState(true);
  const [sampleId, setSampleId] = useState<number | null>(null);
  const [routed, setRouted] = useState<number | null>(null);

  useEffect(() => {
    if (!open) return;
    const d = fromMessage ? draftFromMessage(fromMessage) : null;
    setName(filter?.name ?? "");
    setSender(filter?.sender ?? d?.sender ?? "");
    setSubject(filter?.subject_match ?? d?.subject_match ?? "");
    setBody(filter?.body_match ?? "");
    setRegex(filter?.use_regex ?? false);
    setAccountId(filter ? String(filter.account_id) : "");
    setSign(filter?.sign ?? "debit");
    setParser(filter?.parser ?? "generic");
    setCustom(filter?.custom_parser ?? emptyParser);
    setEnabled(filter?.enabled ?? true);
    setSampleId(fromMessage?.id ?? null);
    setRouted(null);
  }, [open, filter, fromMessage]);

  const draft = useMemo(
    () => ({
      name,
      sender,
      subject_match: subject,
      body_match: body,
      use_regex: regex,
      account_id: Number(accountId) || 0,
      sign,
      parser,
      custom_parser: parser === "custom" ? custom : null,
      enabled,
    }),
    [name, sender, subject, body, regex, accountId, sign, parser, custom, enabled],
  );
  const previewBody = useDebounced({ ...draft, message_id: sampleId ?? 0 }, 300);
  const preview = useQuery({
    queryKey: ["email", "preview", previewBody],
    queryFn: () => api.post<Preview>("/email/filters/preview", previewBody),
    enabled: open && routed === null,
    placeholderData: keepPreviousData,
  });
  const sample = useQuery({ ...messageQuery(sampleId ?? 0), enabled: open && sampleId !== null });

  const save = useEmailMutation(
    async () => {
      const r = filter
        ? await api.patch<{ routed: number }>(`/email/filters/${filter.id}`, draft)
        : await api.post<{ routed: number }>("/email/filters", draft);
      return r.routed;
    },
    (n) => (n > 0 ? setRouted(n) : onOpenChange(false)),
  );

  const selectable = accounts.filter((a) => a.status !== "closed" && a.status !== "ignored" && a.status !== "review");
  const acct = accounts.find((a) => a.id === Number(accountId));
  const p = preview.data;

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      className="md:max-w-4xl"
      title={filter ? "Edit email filter" : "New email filter"}
      description="The first matching filter, top to bottom, decides where an alert goes."
      footer={
        routed !== null ? (
          <Button size="sm" onClick={() => onOpenChange(false)}>
            Done
          </Button>
        ) : (
          <>
            <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button size="sm" type="submit" form="filter-form" loading={save.isPending}>
              Save filter
            </Button>
          </>
        )
      }
    >
      {routed !== null ? (
        <p className="text-sm" data-testid="filter-routed">
          Filter saved. {routed} waiting {routed === 1 ? "email was" : "emails were"} added as transactions.
        </p>
      ) : (
        <div className="grid gap-5 md:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)]">
          <form
            id="filter-form"
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              save.mutate(undefined);
            }}
          >
            <Section>Match emails</Section>
            <Field label="From" value={sender} onChange={(e) => setSender(e.target.value)} placeholder="alerts@bank.com or bank.com" />
            <Field label="Subject contains" value={subject} onChange={(e) => setSubject(e.target.value)} placeholder="Any subject" />
            <Field
              label="Body contains"
              value={body}
              onChange={(e) => setBody(e.target.value)}
              placeholder="e.g. ending in 1234"
              hint="Use this when one bank sends alerts for several cards."
            />
            <Switch label="Use regular expressions" checked={regex} onCheckedChange={setRegex} />
            <FormError error={p?.filter_error || null} />

            <Section>Add to</Section>
            <Select
              label="Account"
              value={accountId}
              onChange={(e) => setAccountId(e.target.value)}
              required
              options={[{ value: "", label: "Choose an account" }, ...selectable.map((a) => ({ value: String(a.id), label: accountLabel(a) }))]}
            />
            {acct && (
              <p className="-mt-1 text-xs text-muted">
                {acct.is_manual
                  ? "Email-only account: alerts are final transactions."
                  : "Synced account: alerts count right away and link to the bank's transaction when it arrives."}
              </p>
            )}
            <Segmented
              label="Alert type"
              value={sign}
              onChange={setSign}
              items={[
                { value: "debit", label: "Purchases (money out)" },
                { value: "credit", label: "Deposits (money in)" },
              ]}
            />

            <Section>Read the alert with</Section>
            <Select
              label="Parser"
              value={parser}
              onChange={(e) => setParser(e.target.value)}
              options={[...(templates ?? []).map((t) => ({ value: t.name, label: t.label })), { value: "custom", label: "Custom…" }]}
            />
            {parser === "custom" && (
              <div className="flex flex-col gap-3 rounded-lg border border-border p-3">
                <p className="text-xs text-muted">
                  For each value, enter the text that comes right before it in the email (like “Amount:”). The value runs to the end of that
                  line, or up to the “after” text.
                </p>
                <SpecEditor label="Amount" spec={custom.amount} onChange={(s) => setCustom({ ...custom, amount: s })} />
                <SpecEditor label="Merchant" spec={custom.merchant} onChange={(s) => setCustom({ ...custom, merchant: s })} />
                <SpecEditor label="Date (optional)" spec={custom.date} onChange={(s) => setCustom({ ...custom, date: s })} />
              </div>
            )}
            <FormError error={p?.parser_error || null} />
            <Field label="Filter name" value={name} onChange={(e) => setName(e.target.value)} placeholder={sender || "Bank alerts"} />
            {filter && <Switch label="Enabled" checked={enabled} onCheckedChange={setEnabled} />}
            <FormError error={save.error} />
          </form>

          <div className="flex min-w-0 flex-col gap-3 md:sticky md:top-0 md:self-start" data-testid="filter-preview">
            {sampleId !== null && (
              <>
                <Section>Sample email</Section>
                <div className="rounded-lg border border-border">
                  <div className="border-b border-border px-3 py-2 text-[13px]">
                    <div className="truncate font-medium">{sample.data?.subject}</div>
                    <div className="truncate text-muted">{sample.data?.from_addr}</div>
                  </div>
                  <pre className="max-h-56 overflow-auto whitespace-pre-wrap break-words px-3 py-2 font-sans text-[13px] text-text">
                    {sample.data?.body_text || (sample.isLoading ? "Loading…" : "The body of this email is no longer stored.")}
                  </pre>
                  {p?.sample && (
                    <div className="border-t border-border px-3 py-2" data-testid="sample-result">
                      <ParseResult m={p.sample} />
                    </div>
                  )}
                </div>
              </>
            )}
            <Section>
              Matching recent emails {p && !p.filter_error && <span className="font-normal normal-case">({p.matches.length})</span>}
            </Section>
            {!p || p.filter_error ? (
              <p className="text-[13px] text-muted">Set a sender, subject or body text to see which emails match.</p>
            ) : p.matches.length === 0 ? (
              <p className="text-[13px] text-muted">No recent emails match. New alerts will still be checked as they arrive.</p>
            ) : (
              <ul className="divide-y divide-border rounded-lg border border-border">
                {p.matches.map((m) => (
                  <li key={m.id}>
                    <button
                      type="button"
                      onClick={() => setSampleId(m.id)}
                      className={clsx("w-full px-3 py-2 text-left hover:bg-surface-2", m.id === sampleId && "bg-surface-2")}
                    >
                      <div className="flex items-center gap-2">
                        <span className="min-w-0 flex-1 truncate text-[13px]">{m.subject || "(no subject)"}</span>
                        {m.status === "parsed" && <Badge>{statusLabels.parsed}</Badge>}
                      </div>
                      <ParseResult m={m} />
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      )}
    </Dialog>
  );
}

function Section({ children }: { children: React.ReactNode }) {
  return <div className="text-[11px] font-semibold uppercase tracking-wide text-muted">{children}</div>;
}

function ParseResult({ m }: { m: PreviewMatch }) {
  if (m.parsed) return <ParsedLine p={m.parsed} />;
  if (m.error) return <p className="text-xs text-negative">Couldn't read: {m.error}</p>;
  return null;
}

function ParsedLine({ p }: { p: Parsed }) {
  return (
    <p className="flex flex-wrap items-center gap-x-2 text-xs text-muted" data-testid="parsed-line">
      <MoneyText cents={p.amount_cents} className="font-medium text-text" />
      <span className="text-text">{p.merchant}</span>
      <span>{new Date(p.date + "T00:00:00").toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" })}</span>
    </p>
  );
}

function SpecEditor({ label, spec, onChange }: { label: string; spec: FieldSpec; onChange: (s: FieldSpec) => void }) {
  const mode = spec.regex !== undefined && spec.regex !== null && spec.before === undefined ? "regex" : "text";
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between gap-2">
        <span className="text-[13px] font-medium">{label}</span>
        <Segmented
          label={`${label} mode`}
          value={mode}
          onChange={(m) => onChange(m === "regex" ? { regex: spec.regex ?? "" } : { before: "", after: "" })}
          items={[
            { value: "text", label: "Text before" },
            { value: "regex", label: "Regex" },
          ]}
        />
      </div>
      {mode === "regex" ? (
        <Field
          label={`${label} regex`}
          className="[&>label]:sr-only"
          value={spec.regex ?? ""}
          onChange={(e) => onChange({ regex: e.target.value })}
          placeholder="First (group) is the value"
        />
      ) : (
        <div className="grid grid-cols-2 gap-2">
          <Field
            label={`${label}: text before`}
            className="[&>label]:sr-only"
            value={spec.before ?? ""}
            onChange={(e) => onChange({ ...spec, before: e.target.value })}
            placeholder="Text before"
          />
          <Field
            label={`${label}: text after`}
            className="[&>label]:sr-only"
            value={spec.after ?? ""}
            onChange={(e) => onChange({ ...spec, after: e.target.value })}
            placeholder="Text after (optional)"
          />
        </div>
      )}
    </div>
  );
}
