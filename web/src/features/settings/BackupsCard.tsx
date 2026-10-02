import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Archive, DatabaseBackup } from "lucide-react";
import { Button, Card, FormError } from "@/components/ui";
import { api } from "@/lib/api";
import { timeAgo } from "@/lib/format";
import { useSession } from "@/lib/session";

type Backup = { name: string; size: number; at: number };
type Backups = { dir: string; keep: number; backups: Backup[] };

const backupsQuery = queryOptions({ queryKey: ["settings", "backups"], queryFn: () => api.get<Backups>("/settings/backups") });

function size(bytes: number) {
  return bytes < 1024 * 1024 ? `${Math.max(1, Math.round(bytes / 1024))} KB` : `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

/** Backup status and "Back up now". Admins only (backups hold the key that decrypts bank tokens). */
export function BackupsCard() {
  const { data: session } = useSession();
  const admin = !!session?.user?.is_admin;
  const qc = useQueryClient();
  const { data } = useQuery({ ...backupsQuery, enabled: admin });
  const create = useMutation({
    mutationFn: () => api.post<Backups>("/settings/backups"),
    onSuccess: (b) => qc.setQueryData(backupsQuery.queryKey, b),
  });
  if (!admin) return null;
  const latest = data?.backups[0];
  return (
    <Card
      title="Backups"
      action={
        <Button size="sm" variant="secondary" loading={create.isPending} onClick={() => create.mutate()}>
          <DatabaseBackup size={14} /> Back up now
        </Button>
      }
    >
      <div className="flex flex-col gap-3">
        <p className="text-sm text-muted">
          {data?.keep
            ? `A backup of the database and keys is made every day; the last ${data.keep} are kept.`
            : "Automatic backups are off (backup.keep = 0 in viceroy.toml)."}{" "}
          Restore one with <span className="font-mono text-[13px]">viceroy restore &lt;file&gt;</span> while the server is stopped.
        </p>
        {data && (
          <div className="text-[13px] text-muted">
            Folder: <span className="font-mono break-all">{data.dir}</span>
          </div>
        )}
        {data && data.backups.length === 0 && <p className="text-[13px] text-muted">No backups yet.</p>}
        {latest && (
          <ul className="divide-y divide-border rounded-lg border border-border" data-testid="backup-list">
            {data.backups.slice(0, 5).map((b) => (
              <li key={b.name} className="flex items-center gap-3 px-3 py-2">
                <Archive size={16} className="shrink-0 text-muted" />
                <div className="min-w-0 flex-1 text-sm">
                  <div className="truncate font-mono text-[13px]">{b.name}</div>
                  <div className="text-xs text-muted">
                    {timeAgo(b.at)} · {size(b.size)}
                  </div>
                </div>
              </li>
            ))}
          </ul>
        )}
        {data && data.backups.length > 5 && <p className="text-xs text-muted">and {data.backups.length - 5} older</p>}
        <FormError error={create.error} />
      </div>
    </Card>
  );
}
