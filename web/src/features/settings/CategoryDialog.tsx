import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Button, Dialog, Field, FormError, Select } from "@/components/ui";
import { categoriesQuery, useTxnMutation, type CategoryGroup } from "@/features/transactions/api";
import { api } from "@/lib/api";

/** A new category in a group, or an existing one to rename, re-icon or delete. */
export type CategoryDraft = { group: CategoryGroup } | { group: CategoryGroup; category: { id: number; name: string; icon: string } };

export function CategoryDialog({ draft, onClose }: { draft: CategoryDraft | null; onClose: () => void }) {
  const cat = draft && "category" in draft ? draft.category : null;
  const { data } = useQuery(categoriesQuery);
  const [name, setName] = useState("");
  const [icon, setIcon] = useState("");
  const [deleting, setDeleting] = useState(false);
  const [moveTo, setMoveTo] = useState("");
  useEffect(() => {
    if (!draft) return;
    setName(cat?.name ?? "");
    setIcon(cat?.icon ?? "");
    setDeleting(false);
    setMoveTo("");
  }, [draft, cat]);

  const usage = useQuery({
    queryKey: ["categories", "usage", cat?.id],
    queryFn: () => api.get<{ transactions: number }>(`/categories/${cat!.id}/usage`),
    enabled: deleting && !!cat,
  });
  const qc = useQueryClient();
  const done = () => {
    qc.invalidateQueries({ queryKey: ["categories"] });
    onClose();
  };
  const save = useTxnMutation(
    () => (cat ? api.patch(`/categories/${cat.id}`, { name, icon }) : api.post("/categories", { name, icon, group_id: draft!.group.id })),
    done,
  );
  const del = useTxnMutation(() => api.del(`/categories/${cat!.id}${moveTo ? `?move_to=${moveTo}` : ""}`), done);
  useEffect(() => {
    if (!draft) {
      save.reset();
      del.reset();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft]);

  const others = (data?.groups ?? []).flatMap((g) => g.categories.filter((c) => c.id !== cat?.id).map((c) => ({ value: String(c.id), label: `${g.name} · ${c.icon} ${c.name}` })));
  const count = usage.data?.transactions ?? 0;

  return (
    <Dialog
      open={draft !== null}
      onOpenChange={(o) => !o && onClose()}
      title={cat ? "Edit category" : `New category in ${draft?.group.name ?? ""}`}
      footer={
        deleting ? (
          <>
            <Button variant="secondary" className="mr-auto" onClick={() => setDeleting(false)}>
              Keep it
            </Button>
            <Button variant="danger" loading={del.isPending} disabled={usage.isLoading} onClick={() => del.mutate(undefined)}>
              Delete category
            </Button>
          </>
        ) : (
          <>
            {cat && (
              <Button variant="danger-ghost" className="mr-auto" aria-label="Delete category" onClick={() => setDeleting(true)}>
                <Trash2 size={14} />
              </Button>
            )}
            <Button variant="secondary" onClick={onClose}>
              Cancel
            </Button>
            <Button onClick={() => save.mutate(undefined)} loading={save.isPending}>
              Save
            </Button>
          </>
        )
      }
    >
      {deleting ? (
        <div className="flex flex-col gap-3" data-testid="category-delete">
          <p className="text-sm">
            {usage.isLoading
              ? "Checking its transactions…"
              : count === 0
                ? `No transactions use ${cat?.name}. Its budget amounts are deleted.`
                : `${count} transaction${count === 1 ? " uses" : "s use"} ${cat?.name}. Move them to another category, or leave them uncategorized. Its budget amounts are deleted.`}
          </p>
          {count > 0 && (
            <Select
              label="Move its transactions and rules to"
              value={moveTo}
              onChange={(e) => setMoveTo(e.target.value)}
              options={[{ value: "", label: "Leave uncategorized" }, ...others]}
            />
          )}
          <FormError error={del.error} />
        </div>
      ) : (
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate(undefined);
          }}
          data-testid="category-dialog"
        >
          <div className="grid grid-cols-[4rem_1fr] gap-3">
            <Field label="Icon" value={icon} onChange={(e) => setIcon(e.target.value)} maxLength={8} className="text-center" placeholder="📦" />
            <Field label="Name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Pet care" autoFocus />
          </div>
          <FormError error={save.error} />
          <button type="submit" hidden />
        </form>
      )}
    </Dialog>
  );
}
