import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, Dialog, Field, FormError, Select, Switch, TextArea } from "@/components/ui";
import { api } from "@/lib/api";
import { useEmailMutation, type EmailAIInfo, type Mailbox, type Security } from "./api";

// Common providers; "Other" leaves the fields to the user.
const presets: Record<string, { host: string; port: number; security: Security; hint: string }> = {
  gmail: { host: "imap.gmail.com", port: 993, security: "tls", hint: "Create an app password under Google Account → Security → App passwords." },
  icloud: { host: "imap.mail.me.com", port: 993, security: "tls", hint: "Create an app-specific password at account.apple.com." },
  outlook: { host: "outlook.office365.com", port: 993, security: "tls", hint: "Use an app password if two-step verification is on." },
  fastmail: { host: "imap.fastmail.com", port: 993, security: "tls", hint: "Create an app password under Settings → Privacy & Security." },
  yahoo: { host: "imap.mail.yahoo.com", port: 993, security: "tls", hint: "Generate an app password in Yahoo account security." },
};

const securityOptions = [
  { value: "tls", label: "SSL/TLS" },
  { value: "starttls", label: "STARTTLS" },
  { value: "none", label: "None (insecure)" },
];

/** Add or edit an IMAP mailbox. Saving tests the login first. */
export function MailboxDialog({ open, onOpenChange, mailbox }: { open: boolean; onOpenChange: (o: boolean) => void; mailbox: Mailbox | null }) {
  const [preset, setPreset] = useState("");
  const [name, setName] = useState("");
  const [host, setHost] = useState("");
  const [port, setPort] = useState("993");
  const [security, setSecurity] = useState<Security>("tls");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [folder, setFolder] = useState("INBOX");
  const [enabled, setEnabled] = useState(true);
  const [aiRead, setAIRead] = useState(false);
  const [aiSenders, setAISenders] = useState("");
  const { data: aiInfo } = useQuery({ queryKey: ["email", "ai"], queryFn: () => api.get<EmailAIInfo>("/email/ai"), enabled: open });

  useEffect(() => {
    if (!open) return;
    setPreset(mailbox ? "" : "gmail");
    setName(mailbox?.name ?? "");
    setHost(mailbox?.host ?? presets.gmail.host);
    setPort(String(mailbox?.port ?? 993));
    setSecurity(mailbox?.security ?? "tls");
    setUsername(mailbox?.username ?? "");
    setPassword("");
    setFolder(mailbox?.folder ?? "INBOX");
    setEnabled(mailbox?.enabled ?? true);
    setAIRead(mailbox?.ai_read ?? false);
    setAISenders(mailbox?.ai_senders ?? "");
  }, [open, mailbox]);

  const choosePreset = (p: string) => {
    setPreset(p);
    if (presets[p]) {
      setHost(presets[p].host);
      setPort(String(presets[p].port));
      setSecurity(presets[p].security);
    }
  };

  const save = useEmailMutation(
    () => {
      const body = { name, host, port: Number(port) || 0, security, username, password, folder, enabled, ai_read: aiRead, ai_senders: aiSenders };
      return mailbox ? api.patch<Mailbox>(`/email/mailboxes/${mailbox.id}`, body) : api.post<Mailbox>("/email/mailboxes", body);
    },
    () => onOpenChange(false),
  );

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={mailbox ? "Edit mailbox" : "Connect a mailbox"}
      description="Viceroy only reads this folder. It never marks, moves or deletes mail."
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button size="sm" type="submit" form="mailbox-form" loading={save.isPending}>
            {mailbox ? "Save" : "Connect"}
          </Button>
        </>
      }
    >
      <form
        id="mailbox-form"
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate(undefined);
        }}
      >
        {!mailbox && (
          <Select
            label="Provider"
            value={preset}
            onChange={(e) => choosePreset(e.target.value)}
            options={[
              { value: "gmail", label: "Gmail" },
              { value: "icloud", label: "iCloud" },
              { value: "outlook", label: "Outlook / Microsoft 365" },
              { value: "fastmail", label: "Fastmail" },
              { value: "yahoo", label: "Yahoo" },
              { value: "", label: "Other (IMAP)" },
            ]}
          />
        )}
        <Field label="Email address or username" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" required />
        <Field
          label="App password"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="new-password"
          placeholder={mailbox ? "Unchanged" : ""}
          required={!mailbox}
          hint={presets[preset]?.hint ?? "Stored encrypted on this server."}
        />
        <Field
          label="Folder or label"
          value={folder}
          onChange={(e) => setFolder(e.target.value)}
          hint="Tip: have your mail app file bank alerts into their own folder (e.g. “Bank alerts”) and watch only that."
        />
        {(preset === "" || mailbox) && (
          <>
            <div className="grid grid-cols-[1fr_6rem] gap-2">
              <Field label="IMAP server" value={host} onChange={(e) => setHost(e.target.value)} placeholder="imap.example.com" required />
              <Field label="Port" inputMode="numeric" value={port} onChange={(e) => setPort(e.target.value)} />
            </div>
            <Select label="Security" value={security} onChange={(e) => setSecurity(e.target.value as Security)} options={securityOptions} />
          </>
        )}
        <Field label="Name" value={name} onChange={(e) => setName(e.target.value)} placeholder={username || "Bank alerts"} />
        {mailbox && <Switch label="Watch this mailbox" checked={enabled} onCheckedChange={setEnabled} />}
        <div className="flex flex-col gap-3 border-t border-border pt-4">
          <Switch
            label="Read unmatched emails with AI"
            hint={
              aiInfo?.configured
                ? `Bank emails no filter caught (payment reminders, scheduled payments, security alerts) are read by ${aiInfo.model}${aiInfo.local ? " on your own server" : " through OpenRouter"}. Leave off if this is your everyday inbox and you'd rather not send it.`
                : "Needs an OpenRouter API key: add one in Settings → AI."
            }
            checked={aiRead}
            disabled={!aiInfo?.configured && !aiRead}
            onCheckedChange={setAIRead}
          />
          {aiRead && (
            <TextArea
              label="Only from these senders"
              rows={3}
              value={aiSenders}
              onChange={(e) => setAISenders(e.target.value)}
              placeholder={"chase.com\nalerts@mybank.com"}
              hint="One address or domain per line (a domain covers its subdomains). Empty = every unmatched email, only for a mailbox that gets nothing but bank email."
            />
          )}
        </div>
        <FormError error={save.error} />
      </form>
    </Dialog>
  );
}
