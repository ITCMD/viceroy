import { queryOptions } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type Member = { id: number; name: string; email: string; is_admin: boolean; joined_at: number; is_you: boolean };
export type Invite = {
  id: number;
  kind: "join" | "reset";
  label: string;
  user_id: number | null;
  user_name: string;
  created_by: string;
  created_at: number;
  expires_at: number;
};
export type HouseholdInfo = { id: number; name: string; members: Member[]; invites: Invite[]; can_manage: boolean };
export type NewInvite = { id: number; kind: "join" | "reset"; token: string; url: string; expires_at: number };
export type InviteInfo = { kind: "join" | "reset"; household_name: string; invited_by: string; email: string; expires_at: number };

export const householdQuery = queryOptions({ queryKey: ["household"], queryFn: () => api.get<HouseholdInfo>("/household") });

/** The link to hand out: the server's public_url when set, else this app's address. */
export const inviteLink = (i: NewInvite) => i.url || `${window.location.origin}/join/${i.token}`;

/** First names for compact owner labels ("Sam"), full name when two members share one. */
export function ownerName(members: Member[], id: number | null | undefined) {
  const m = members.find((x) => x.id === id);
  if (!m) return "Shared";
  const first = m.name.split(" ")[0];
  return members.filter((x) => x.name.split(" ")[0] === first).length > 1 ? m.name : first;
}
