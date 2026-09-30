import { Badge } from "@/components/ui";
import type { Account } from "./api";

export function StatusBadge({ account: a }: { account: Account }) {
  if (a.status === "review") return <Badge tone="warning">Needs review</Badge>;
  if (a.status === "disconnected") return <Badge tone="negative">Disconnected</Badge>;
  if (a.status === "ignored") return <Badge>Ignored</Badge>;
  if (a.status === "closed") return <Badge>Closed</Badge>;
  if (a.institution_status === "reauth") return <Badge tone="negative">Reconnect on SimpleFIN</Badge>;
  return null;
}
