import { keepPreviousData, useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { AlertTriangle, CheckCircle2, FlaskConical, Sparkles, XCircle } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Badge, Button, Dialog, Field, FormError, MoneyText, Segmented, Select, Switch } from "@/components/ui";
import { accountLabel, type Account } from "@/features/accounts/api";
import { api } from "@/lib/api";
import {
  draftFromMessage,
  messageQuery,
  plausibleMerchant,
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

const dateLabel = (d: string) => new Date(d + "T00:00:00").toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });

const parserHints: Record<string, string> = {
  generic: "Finds the amount, merchant and date on its own. Works for most banks.",
  custom: "Tell Viceroy which text comes right before (and after) each value, like “Amount:”.",
};

/**
 * Create or edit an email filter as a short guided flow: which emails, which account, how to
 * read them, then a test on a real email. Starts from the AI's reading of the email and the
 * account its last 4 digits point to, when there is one.
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
  const [testedKey, setTestedKey] = useState<string | null>(null);
  const [showEmail, setShowEmail] = useState(false);

  const sample = useQuery({ ...messageQuery(sampleId ?? 0), enabled: open && sampleId !== null });
  const seededFor = useRef(0);
  const s = sample.data;

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
    setTestedKey(null);
    setShowEmail(false);
    seededFor.current = 0;
  }, [open, filter, fromMessage]);

  // A new filter from an email starts from what's known about it: the AI's reading (which
  // Viceroy checked), the account its last 4 digits point to, and whether money came in.
  useEffect(() => {
    if (!open || filter || !s || seededFor.current === s.id) return;
    seededFor.current = s.id;
    const rc = s.ai_recipe;
    if (s.suggested_account_id) setAccountId(String(s.suggested_account_id));
    setSign(s.suggested_sign);
    if (s.account_phrase) setBody(s.account_phrase);
    if (rc) {
      if (rc.recipe.subject_contains && s.subject.toLowerCase().includes(rc.recipe.subject_contains.toLowerCase())) setSubject(rc.recipe.subject_contains);
      if (rc.parser === "custom" && rc.custom_parser) {
        setParser("custom");
        setCustom(JSON.parse(rc.custom_parser));
      } else if (rc.parser) {
        setParser(rc.parser);
      } else if (rc.recipe.amount_rule && rc.recipe.merchant_rule) {
        setCustom({ amount: rc.recipe.amount_rule, merchant: rc.recipe.merchant_rule, date: rc.recipe.date_rule ?? {} });
      }
    }
  }, [open, filter, s]);

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
  // Test on the email the filter was opened from, else on the first recent email it catches.
  const testMatch = p?.sample ?? p?.matches[0] ?? null;
  const draftKey = JSON.stringify(previewBody);
  const tested = testedKey !== null;
  const stale = tested && testedKey !== draftKey;
  const domain = s?.from_addr.split("@")[1];
  const rc = !filter ? s?.ai_recipe : null;
  const parserOptions = [
    { value: "generic", label: "Automatic (recommended)" },
    ...(templates ?? []).filter((t) => t.name !== "generic").map((t) => ({ value: t.name, label: `${t.label} template` })),
    { value: "custom", label: "Point to the values myself" },
  ];

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      className="md:max-w-2xl"
      title={filter ? "Edit email filter" : "New email filter"}
      description="Teach Viceroy to turn emails like this one into transactions."
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
        <form
          id="filter-form"
          className="flex flex-col gap-5"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate(undefined);
          }}
        >
          {filter?.source === "ai" && (
            <Note icon={<Sparkles size={14} />}>
              The AI wrote this filter, and Viceroy checked it on the email it came from. Transactions it adds are marked for review. Saving makes it
              yours: the AI won't change it after that.
            </Note>
          )}
          {rc && (
            <Note icon={<Sparkles size={14} />} testId="filter-ai-reading">
              AI read this email: <b>${rc.recipe.amount}</b> {rc.recipe.direction === "in" ? "from" : "to"} <b>{rc.recipe.merchant || "?"}</b>
              {rc.recipe.account_text && <> ({rc.recipe.account_text})</>}. The steps below start from what it found
              {rc.problem ? `; it couldn't finish on its own because ${rc.problem}.` : "."}
            </Note>
          )}

          {s && (
            <div className="rounded-lg border border-border">
              <button type="button" className="flex w-full items-center gap-2 px-3 py-2 text-left text-[13px]" onClick={() => setShowEmail(!showEmail)} aria-expanded={showEmail}>
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-medium">{s.subject || "(no subject)"}</span>
                  <span className="block truncate text-muted">{s.from_addr}</span>
                </span>
                <span className="shrink-0 text-accent">{showEmail ? "Hide email" : "Show email"}</span>
              </button>
              {showEmail && (
                <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words border-t border-border px-3 py-2 font-sans text-[13px] text-text">
                  {s.body_text || "The body of this email is no longer stored."}
                </pre>
              )}
            </div>
          )}

          <Step n={1} title="Which emails?">
            <div className="flex flex-col gap-1">
              <Field label="From" value={sender} onChange={(e) => setSender(e.target.value)} placeholder="alerts@bank.com or bank.com" />
              {s && (
                <div className="flex flex-wrap gap-1.5">
                  <Chip active={sender === s.from_addr.toLowerCase()} onClick={() => setSender(s.from_addr.toLowerCase())}>
                    Only {s.from_addr}
                  </Chip>
                  {domain && (
                    <Chip active={sender === domain} onClick={() => setSender(domain)}>
                      Anything from {domain}
                    </Chip>
                  )}
                </div>
              )}
            </div>
            <Field label="Subject includes" value={subject} onChange={(e) => setSubject(e.target.value)} placeholder="Any subject" hint="Leave empty to match any subject." />
            <div className="flex flex-col gap-1">
              <Field
                label="Email mentions (optional)"
                value={body}
                onChange={(e) => setBody(e.target.value)}
                placeholder="e.g. ending in 1234"
                hint="Add the account's last 4 digits when this bank emails you about more than one account."
              />
              {s?.account_phrase && body !== s.account_phrase && (
                <div>
                  <Chip onClick={() => setBody(s.account_phrase)}>Use “{s.account_phrase}”</Chip>
                </div>
              )}
            </div>
            <details className="text-[13px]" open={regex}>
              <summary className="cursor-pointer text-muted">Advanced</summary>
              <div className="mt-2">
                <Switch
                  label="Match patterns instead of plain text"
                  hint="For advanced users: the subject and mention texts become regular expressions, so Withdrawal|Deposit matches either word. Leave off to match the text as typed."
                  checked={regex}
                  onCheckedChange={setRegex}
                />
              </div>
            </details>
            <FormError error={p?.filter_error || null} />
          </Step>

          <Step n={2} title="Where does it go?">
            <Select
              label="Account"
              value={accountId}
              onChange={(e) => setAccountId(e.target.value)}
              required
              options={[{ value: "", label: "Choose an account" }, ...selectable.map((a) => ({ value: String(a.id), label: accountLabel(a) }))]}
            />
            {acct && s?.suggested_account_id === acct.id && (
              <p className="-mt-1.5 text-xs text-positive">Found {acct.mask ? `…${acct.mask}` : "this account"} in the email.</p>
            )}
            {acct && (
              <p className="-mt-1.5 text-xs text-muted">
                {acct.is_manual
                  ? "Email-only account: these become final transactions."
                  : "Synced account: these count right away and link to the bank's transaction when it arrives."}
              </p>
            )}
            <Segmented
              label="Money"
              value={sign}
              onChange={setSign}
              items={[
                { value: "debit", label: "Money out" },
                { value: "credit", label: "Money in" },
              ]}
            />
            <p className="-mt-1.5 text-xs text-muted">{sign === "credit" ? "Deposits, refunds and transfers in." : "Purchases, withdrawals and payments."}</p>
          </Step>

          <Step n={3} title="How should Viceroy read it?">
            <Select label="Read the amount, merchant and date" value={parser} onChange={(e) => setParser(e.target.value)} options={parserOptions} />
            <p className="-mt-1.5 text-xs text-muted">{parserHints[parser] ?? "Made for this bank's alert emails."}</p>
            {parser === "custom" && (
              <div className="flex flex-col gap-3 rounded-lg border border-border p-3">
                <SpecEditor label="Amount" spec={custom.amount} onChange={(v) => setCustom({ ...custom, amount: v })} example="Amount:" />
                <SpecEditor label="Merchant" spec={custom.merchant} onChange={(v) => setCustom({ ...custom, merchant: v })} example="Merchant:" />
                <SpecEditor label="Date (optional)" spec={custom.date} onChange={(v) => setCustom({ ...custom, date: v })} example="Date:" />
              </div>
            )}
            <FormError error={p?.parser_error || null} />
          </Step>

          <Step n={4} title="Test it">
            <div className="flex flex-wrap items-center gap-2">
              <Button type="button" variant="secondary" size="sm" disabled={!testMatch} onClick={() => setTestedKey(draftKey)}>
                <FlaskConical size={14} /> {stale ? "Test again" : sampleId ? "Test on this email" : "Test on a recent email"}
              </Button>
              {!testMatch && <span className="text-xs text-muted">Needs an email to test on: set “From” to a sender that has emailed you.</span>}
              {stale && <span className="text-xs text-muted">Changed since the last test.</span>}
            </div>
            {tested && testMatch && (
              <TestResult m={testMatch} sign={sign} account={acct ? accountLabel(acct) : ""} loading={preview.isFetching} onCustom={() => setParser("custom")} />
            )}
            {tested && p && !p.filter_error && (
              <details className="text-[13px]">
                <summary className="cursor-pointer text-muted">
                  Recent emails this filter catches ({p.matches.length})
                </summary>
                {p.matches.length === 0 ? (
                  <p className="mt-1 text-muted">None yet. New emails are still checked as they arrive.</p>
                ) : (
                  <ul className="mt-1 divide-y divide-border rounded-lg border border-border">
                    {p.matches.map((m) => (
                      <li key={m.id}>
                        <button
                          type="button"
                          onClick={() => setSampleId(m.id)}
                          className={clsx("w-full px-3 py-2 text-left hover:bg-surface-2", m.id === sampleId && "bg-surface-2")}
                        >
                          <div className="flex items-center gap-2">
                            <span className="min-w-0 flex-1 truncate">{m.subject || "(no subject)"}</span>
                            {m.status === "parsed" && <Badge>{statusLabels.parsed}</Badge>}
                          </div>
                          {m.parsed ? <ParsedLine p={m.parsed} /> : m.error && <p className="text-xs text-negative">Couldn't read: {m.error}</p>}
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </details>
            )}
          </Step>

          <div className="flex flex-col gap-3 border-t border-border pt-4">
            <Field label="Filter name (optional)" value={name} onChange={(e) => setName(e.target.value)} placeholder={sender || "Bank alerts"} />
            {filter && <Switch label="Enabled" checked={enabled} onCheckedChange={setEnabled} />}
            <FormError error={save.error} />
          </div>
        </form>
      )}
    </Dialog>
  );
}

function Step({ n, title, children }: { n: number; title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-3" aria-label={title}>
      <h3 className="flex items-center gap-2 text-[14px] font-semibold">
        <span className="grid size-5 place-items-center rounded-full bg-accent-soft text-[11px] text-accent">{n}</span>
        {title}
      </h3>
      {children}
    </section>
  );
}

function Note({ icon, children, testId }: { icon: ReactNode; children: ReactNode; testId?: string }) {
  return (
    <p className="flex gap-2 rounded-lg bg-accent-soft px-3 py-2 text-[13px]" data-testid={testId}>
      <span className="mt-0.5 shrink-0 text-accent">{icon}</span>
      <span>{children}</span>
    </p>
  );
}

function Chip({ active, onClick, children }: { active?: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={clsx(
        "max-w-full truncate rounded-full border px-2.5 py-0.5 text-xs",
        active ? "border-accent bg-accent-soft text-accent" : "border-border text-muted hover:bg-surface-2 hover:text-text",
      )}
    >
      {children}
    </button>
  );
}

/** A checklist of what the filter does with one email. */
function TestResult({ m, sign, account, loading, onCustom }: { m: PreviewMatch; sign: string; account: string; loading: boolean; onCustom: () => void }) {
  const merchantOK = !!m.parsed && plausibleMerchant(m.parsed.merchant);
  const all = m.matches && !!m.parsed && merchantOK && !!account;
  return (
    <div className={clsx("rounded-lg border px-3 py-2.5", all ? "border-positive/40" : "border-border", loading && "opacity-60")} data-testid="filter-test-result">
      <div className="mb-1.5 truncate text-xs text-muted">Tested on “{m.subject || "(no subject)"}”</div>
      <ul className="flex flex-col gap-1.5 text-[13px]">
        <Check ok={m.matches} label={m.matches ? "This email matches the filter" : "This email doesn't match: check From, Subject and Mentions above"} />
        {m.parsed ? (
          <>
            <Check
              ok
              label={
                <>
                  Amount <MoneyText cents={m.parsed.amount_cents} className="font-medium" /> · {sign === "credit" ? "money in" : "money out"}
                </>
              }
            />
            <Check
              ok={merchantOK}
              warn={!merchantOK}
              label={
                merchantOK ? (
                  <>
                    Merchant <span className="font-medium">{m.parsed.merchant}</span>
                  </>
                ) : (
                  <>
                    Merchant “{m.parsed.merchant}” looks wrong.{" "}
                    <button type="button" className="font-medium text-accent hover:underline" onClick={onCustom}>
                      Point to it yourself
                    </button>
                  </>
                )
              }
            />
            <Check ok label={<>Date {dateLabel(m.parsed.date)}</>} />
          </>
        ) : (
          <Check
            ok={false}
            label={
              <>
                Couldn't read it: {m.error || "pick how to read it above"}.{" "}
                <button type="button" className="font-medium text-accent hover:underline" onClick={onCustom}>
                  Point to the values yourself
                </button>
              </>
            }
          />
        )}
        <Check ok={!!account} label={account ? <>Goes to {account}</> : "Choose an account above"} />
      </ul>
    </div>
  );
}

function Check({ ok, warn, label }: { ok: boolean; warn?: boolean; label: ReactNode }) {
  const Icon = ok ? CheckCircle2 : warn ? AlertTriangle : XCircle;
  return (
    <li className="flex items-start gap-2">
      <Icon size={15} className={clsx("mt-0.5 shrink-0", ok ? "text-positive" : warn ? "text-accent" : "text-negative")} />
      <span>{label}</span>
    </li>
  );
}

function ParsedLine({ p }: { p: Parsed }) {
  return (
    <p className="flex flex-wrap items-center gap-x-2 text-xs text-muted" data-testid="parsed-line">
      <MoneyText cents={p.amount_cents} className="font-medium text-text" />
      <span className="text-text">{p.merchant}</span>
      <span>{dateLabel(p.date)}</span>
    </p>
  );
}

function SpecEditor({ label, spec, onChange, example }: { label: string; spec: FieldSpec; onChange: (s: FieldSpec) => void; example: string }) {
  const pattern = spec.regex !== undefined && spec.regex !== null && spec.before === undefined;
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between gap-2">
        <span className="text-[13px] font-medium">{label}</span>
        <button
          type="button"
          className="text-xs text-muted hover:text-text"
          onClick={() => onChange(pattern ? { before: "", after: "" } : { regex: spec.regex ?? "" })}
        >
          {pattern ? "Use text before/after" : "Use a pattern (advanced)"}
        </button>
      </div>
      {pattern ? (
        <Field
          label={`${label} pattern`}
          hideLabel
          value={spec.regex ?? ""}
          onChange={(e) => onChange({ regex: e.target.value })}
          placeholder="Regular expression; the part in (parentheses) is the value"
        />
      ) : (
        <div className="grid grid-cols-2 gap-2">
          <Field
            label={`${label}: text right before it`}
            hideLabel
            value={spec.before ?? ""}
            onChange={(e) => onChange({ ...spec, before: e.target.value })}
            placeholder={`Text before, e.g. ${example}`}
          />
          <Field
            label={`${label}: text right after it`}
            hideLabel
            value={spec.after ?? ""}
            onChange={(e) => onChange({ ...spec, after: e.target.value })}
            placeholder="Text after (optional)"
          />
        </div>
      )}
    </div>
  );
}
