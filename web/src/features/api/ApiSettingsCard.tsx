import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Check, Copy, KeyRound, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { Badge, Button, Card, Dialog, Field, FormError, Select, Switch } from "@/components/ui";
import { api } from "@/lib/api";
import { timeAgo } from "@/lib/format";
import { apiSettingsQuery, type ApiKey, type ApiSettings } from "./api";

/** REST API switch, key generator and link to the docs. Admins only. */
export function ApiSettingsCard() {
  const qc = useQueryClient();
  const { data } = useQuery(apiSettingsQuery);
  const [creating, setCreating] = useState(false);
  const toggle = useMutation({
    mutationFn: (enabled: boolean) => api.patch<ApiSettings>("/settings/api", { enabled }),
    onSuccess: (s) => qc.setQueryData(apiSettingsQuery.queryKey, s),
  });
  const revoke = useMutation({
    mutationFn: (id: number) => api.del(`/settings/api/keys/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: apiSettingsQuery.queryKey }),
  });
  const canEdit = !!data?.can_edit;

  return (
    <Card
      title="API"
      action={
        <Link to={"/settings/api-docs" as string} className="text-[13px] font-medium text-accent hover:underline">
          API documentation
        </Link>
      }
    >
      <div className="flex flex-col gap-4">
        <Switch
          label="Enable the REST API"
          hint="Lets scripts and other apps (Home Assistant, spreadsheets…) use Viceroy with an API key. Off turns every key off."
          checked={data?.enabled ?? false}
          disabled={!canEdit || toggle.isPending}
          onCheckedChange={(v) => toggle.mutate(v)}
        />
        {data && !canEdit && <p className="text-[13px] text-muted">Only an admin can manage API access.</p>}
        {canEdit && (
          <div className="flex flex-col gap-2 border-t border-border pt-4">
            <div className="flex items-center justify-between gap-2">
              <div className="text-[13px] font-medium">API keys</div>
              <Button size="sm" variant="secondary" onClick={() => setCreating(true)}>
                <Plus size={14} /> Generate key
              </Button>
            </div>
            {data.keys.length === 0 ? (
              <p className="text-[13px] text-muted">No keys yet.</p>
            ) : (
              <ul className="divide-y divide-border rounded-lg border border-border">
                {data.keys.map((k: ApiKey) => (
                  <li key={k.id} className="flex items-center gap-3 px-3 py-2" data-testid="api-key-row">
                    <KeyRound size={16} className="shrink-0 text-muted" />
                    <div className="min-w-0 flex-1 text-sm">
                      <div className="flex items-center gap-2">
                        <span className="truncate font-medium">{k.name}</span>
                        <Badge tone={k.scope === "write" ? "warning" : "neutral"}>{k.scope === "write" ? "Read & write" : "Read only"}</Badge>
                      </div>
                      <div className="truncate text-xs text-muted">
                        <span className="font-mono">{k.prefix}…</span> · by {k.created_by} · {k.last_used_at ? `used ${timeAgo(k.last_used_at)}` : "never used"}
                      </div>
                    </div>
                    <Button size="sm" variant="danger-ghost" aria-label={`Revoke ${k.name}`} loading={revoke.isPending && revoke.variables === k.id} onClick={() => revoke.mutate(k.id)}>
                      <Trash2 size={14} />
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
        <FormError error={toggle.error ?? revoke.error} />
      </div>
      <NewKeyDialog open={creating} onOpenChange={setCreating} apiOn={!!data?.enabled} />
    </Card>
  );
}

function NewKeyDialog({ open, onOpenChange, apiOn }: { open: boolean; onOpenChange: (v: boolean) => void; apiOn: boolean }) {
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [scope, setScope] = useState<"read" | "write">("read");
  const [copied, setCopied] = useState(false);
  const create = useMutation({
    mutationFn: () => api.post<{ key: string }>("/settings/api/keys", { name, scope }),
    onSuccess: () => qc.invalidateQueries({ queryKey: apiSettingsQuery.queryKey }),
  });
  const close = (v: boolean) => {
    onOpenChange(v);
    if (!v)
      setTimeout(() => {
        setName("");
        setScope("read");
        setCopied(false);
        create.reset();
      }, 200);
  };
  const key = create.data?.key;
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(key!);
      setCopied(true);
    } catch {
      // Clipboard needs HTTPS; the key is selectable anyway.
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={close}
      title={key ? "Your new API key" : "Generate API key"}
      footer={
        key ? (
          <Button onClick={() => close(false)}>Done</Button>
        ) : (
          <>
            <Button variant="secondary" onClick={() => close(false)}>
              Cancel
            </Button>
            <Button loading={create.isPending} disabled={!name.trim()} onClick={() => create.mutate()}>
              Generate
            </Button>
          </>
        )
      }
    >
      {key ? (
        <div className="flex flex-col gap-3">
          <p className="text-sm">Copy it now: it won't be shown again.</p>
          <div className="flex items-center gap-2">
            <code className="min-w-0 flex-1 select-all break-all rounded-lg bg-surface-2 px-3 py-2 font-mono text-[13px]" data-testid="new-api-key">
              {key}
            </code>
            <Button size="sm" variant="secondary" onClick={copy} aria-label="Copy key">
              {copied ? <Check size={14} /> : <Copy size={14} />}
            </Button>
          </div>
          <p className="text-xs text-muted">
            Send it as <code className="font-mono">Authorization: Bearer {key.slice(0, 9)}…</code>.
            {!apiOn && " Turn on the REST API in Settings for it to work."}
          </p>
        </div>
      ) : (
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim()) create.mutate();
          }}
        >
          <Field label="Name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Home Assistant" hint="What will use this key, so you know which to revoke." autoFocus />
          <Select
            label="Access"
            value={scope}
            onChange={(e) => setScope(e.target.value as "read" | "write")}
            options={[
              { value: "read", label: "Read only" },
              { value: "write", label: "Read & write" },
            ]}
          />
          <FormError error={create.error} />
        </form>
      )}
    </Dialog>
  );
}
