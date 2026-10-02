import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, KeyRound, Link2, ShieldCheck, ShieldOff, Trash2, UserMinus, UserPlus } from "lucide-react";
import { useEffect, useState } from "react";
import { Badge, Button, Card, Dialog, Field, FormError, Menu } from "@/components/ui";
import { api } from "@/lib/api";
import { timeAgo, timeUntil } from "@/lib/format";
import { householdQuery, inviteLink, type HouseholdInfo, type Member, type NewInvite } from "./api";

/** Settings → Household: name, members and their roles, join and password reset links. */
export function HouseholdSettings() {
  const { data } = useQuery(householdQuery);
  return (
    <>
      <NameCard data={data} />
      <MembersCard data={data} />
    </>
  );
}

function useHouseholdMutation<T, R = unknown>(fn: (v: T) => Promise<R>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: (res) => {
      if (res && typeof res === "object" && "members" in res) qc.setQueryData(householdQuery.queryKey, res as unknown as HouseholdInfo);
      else qc.invalidateQueries({ queryKey: householdQuery.queryKey });
      // The name and admin flag also live in the session.
      qc.invalidateQueries({ queryKey: ["session"] });
      qc.invalidateQueries({ queryKey: ["wishlist"] });
    },
  });
}

function NameCard({ data }: { data?: HouseholdInfo }) {
  const [name, setName] = useState("");
  useEffect(() => setName(data?.name ?? ""), [data?.name]);
  const save = useHouseholdMutation((n: string) => api.patch<HouseholdInfo>("/household", { name: n }));
  const dirty = !!data && name.trim() !== "" && name.trim() !== data.name;
  return (
    <Card title="Household">
      <form
        className="flex items-end gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (dirty) save.mutate(name.trim());
        }}
      >
        <div className="flex-1">
          <Field
            label="Name"
            value={name}
            disabled={!data?.can_manage}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        {data?.can_manage && (
          <Button type="submit" variant="secondary" disabled={!dirty} loading={save.isPending}>
            Save
          </Button>
        )}
      </form>
      <p className="mt-1 text-xs text-muted">Everyone in the household shares accounts, transactions, the budget and goals.</p>
      <FormError error={save.error} />
    </Card>
  );
}

function MembersCard({ data }: { data?: HouseholdInfo }) {
  const [inviting, setInviting] = useState<{ user?: Member } | null>(null);
  const [removing, setRemoving] = useState<Member | null>(null);
  const setAdmin = useHouseholdMutation((v: { id: number; is_admin: boolean }) => api.patch<HouseholdInfo>(`/household/members/${v.id}`, { is_admin: v.is_admin }));
  const revoke = useHouseholdMutation((id: number) => api.del(`/household/invites/${id}`));
  const manage = !!data?.can_manage;
  const admins = data?.members.filter((m) => m.is_admin).length ?? 0;

  return (
    <Card
      title="Members"
      action={
        manage && (
          <Button size="sm" variant="secondary" onClick={() => setInviting({})}>
            <UserPlus size={14} /> Invite someone
          </Button>
        )
      }
    >
      <ul className="-mx-4 -my-4 divide-y divide-border">
        {data?.members.map((m) => (
          <li key={m.id} className="flex items-center gap-3 px-4 py-3" data-testid="member-row">
            <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-accent-soft text-[13px] font-semibold text-accent">
              {m.name.slice(0, 1).toUpperCase()}
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2 text-sm">
                <span className="truncate font-medium">{m.name}</span>
                {m.is_you && <Badge>You</Badge>}
                {m.is_admin && <Badge tone="accent">Admin</Badge>}
              </div>
              <div className="truncate text-xs text-muted">
                {m.email}
                {m.joined_at > 0 && <> · joined {timeAgo(m.joined_at)}</>}
              </div>
            </div>
            {manage && (
              <Menu
                label={`Manage ${m.name}`}
                items={[
                  m.is_admin
                    ? { label: "Remove admin", icon: ShieldOff, disabled: admins <= 1, onSelect: () => setAdmin.mutate({ id: m.id, is_admin: false }) }
                    : { label: "Make admin", icon: ShieldCheck, onSelect: () => setAdmin.mutate({ id: m.id, is_admin: true }) },
                  { label: "Password reset link", icon: KeyRound, onSelect: () => setInviting({ user: m }) },
                  ...(m.is_you ? [] : [{ label: "Remove from household", icon: UserMinus, onSelect: () => setRemoving(m) }]),
                ]}
              />
            )}
          </li>
        ))}
      </ul>
      {manage && data.invites.length > 0 && (
        <div className="-mx-4 -mb-4 mt-4 border-t border-border">
          <div className="px-4 pb-1 pt-3 text-[13px] font-medium">Open links</div>
          <ul className="divide-y divide-border">
            {data.invites.map((i) => (
              <li key={i.id} className="flex items-center gap-3 px-4 py-2" data-testid="invite-row">
                {i.kind === "reset" ? <KeyRound size={16} className="shrink-0 text-muted" /> : <Link2 size={16} className="shrink-0 text-muted" />}
                <div className="min-w-0 flex-1 text-sm">
                  <div className="truncate">{i.kind === "reset" ? `Password reset for ${i.user_name}` : i.label ? `Invite for ${i.label}` : "Invite link"}</div>
                  <div className="truncate text-xs text-muted">
                    by {i.created_by} · expires {timeUntil(i.expires_at)}
                  </div>
                </div>
                <Button size="sm" variant="danger-ghost" aria-label="Cancel link" loading={revoke.isPending && revoke.variables === i.id} onClick={() => revoke.mutate(i.id)}>
                  <Trash2 size={14} />
                </Button>
              </li>
            ))}
          </ul>
        </div>
      )}
      {data && !manage && <p className="mt-4 text-[13px] text-muted">Only an admin can invite or remove members.</p>}
      <FormError error={setAdmin.error ?? revoke.error} />
      <InviteDialog target={inviting} onClose={() => setInviting(null)} />
      <RemoveDialog member={removing} onClose={() => setRemoving(null)} />
    </Card>
  );
}

function InviteDialog({ target, onClose }: { target: { user?: Member } | null; onClose: () => void }) {
  const [label, setLabel] = useState("");
  const [copied, setCopied] = useState(false);
  const user = target?.user;
  const create = useHouseholdMutation((_: void) => api.post<NewInvite>("/household/invites", user ? { user_id: user.id } : { label: label.trim() }));
  const close = () => {
    onClose();
    setTimeout(() => {
      setLabel("");
      setCopied(false);
      create.reset();
    }, 200);
  };
  const link = create.data ? inviteLink(create.data) : "";
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(link);
      setCopied(true);
    } catch {
      // Clipboard needs HTTPS; the link is selectable anyway.
    }
  };

  return (
    <Dialog
      open={!!target}
      onOpenChange={(v) => !v && close()}
      title={user ? `Password reset for ${user.name}` : "Invite someone"}
      footer={
        link ? (
          <Button onClick={close}>Done</Button>
        ) : (
          <>
            <Button variant="secondary" onClick={close}>
              Cancel
            </Button>
            <Button loading={create.isPending} onClick={() => create.mutate()}>
              Create link
            </Button>
          </>
        )
      }
    >
      {link ? (
        <div className="flex flex-col gap-3">
          <p className="text-sm">
            {user ? `Send this to ${user.name}. It lets them choose a new password and signs them out everywhere else.` : "Send this to the person you're inviting. They pick their name, email and password."}{" "}
            It works once, for 7 days.
          </p>
          <div className="flex items-center gap-2">
            <code className="min-w-0 flex-1 select-all break-all rounded-lg bg-surface-2 px-3 py-2 font-mono text-[13px]" data-testid="invite-link">
              {link}
            </code>
            <Button size="sm" variant="secondary" onClick={copy} aria-label="Copy link">
              {copied ? <Check size={14} /> : <Copy size={14} />}
            </Button>
          </div>
          <p className="text-xs text-muted">They'll need to reach this server, so its address has to be allowed in viceroy.toml.</p>
        </div>
      ) : (
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          {user ? (
            <p className="text-sm">Make a one-time link {user.name} can use to set a new password. Their current password keeps working until they use it.</p>
          ) : (
            <>
              <p className="text-sm">Make a one-time link that adds someone to this household. They'll share your accounts, transactions, budget and goals.</p>
              <Field label="Who's it for?" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Optional, e.g. Sam" autoFocus />
            </>
          )}
          <FormError error={create.error} />
        </form>
      )}
    </Dialog>
  );
}

function RemoveDialog({ member, onClose }: { member: Member | null; onClose: () => void }) {
  const remove = useHouseholdMutation((id: number) => api.del(`/household/members/${id}`));
  return (
    <Dialog
      open={!!member}
      onOpenChange={(v) => !v && onClose()}
      title={`Remove ${member?.name ?? ""}?`}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="danger" loading={remove.isPending} onClick={() => member && remove.mutate(member.id, { onSuccess: onClose })}>
            Remove
          </Button>
        </>
      }
    >
      <p className="text-sm">
        {member?.name} won't be able to sign in any more. Accounts and transactions they own stay in the household as shared. Their chats, API keys and notification settings are deleted.
      </p>
      <FormError error={remove.error} />
    </Dialog>
  );
}
