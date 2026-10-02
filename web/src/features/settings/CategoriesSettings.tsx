import { useQuery, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { ArrowDown, ArrowUp, GripVertical, Pencil, Plus } from "lucide-react";
import { useEffect, useState, type DragEvent } from "react";
import { Button, Card, CategoryIcon, FormError, Select } from "@/components/ui";
import { categoriesQuery, useTxnMutation, type CategoryGroup } from "@/features/transactions/api";
import { api } from "@/lib/api";
import { CategoryDialog, type CategoryDraft } from "./CategoryDialog";

/** Expense groups a category can move between; income and transfers only reorder. */
const movable = new Set(["fixed", "flexible", "non_monthly"]);

const groupHints: Record<string, string> = {
  fixed: "Bills that cost about the same every month.",
  flexible: "Day-to-day spending that changes month to month.",
  non_monthly: "Spending that comes once in a while. Unspent budget and overspending carry over into the next month.",
};

type Drag = { id: number; from: number };

/** Settings → Categories: reorder categories and move them between Fixed, Flexible and Non-monthly. */
export function CategoriesSettings() {
  const qc = useQueryClient();
  const { data } = useQuery(categoriesQuery);
  const [groups, setGroups] = useState<CategoryGroup[]>([]);
  const [drag, setDrag] = useState<Drag | null>(null);
  const [dropAt, setDropAt] = useState<{ group: number; index: number } | null>(null);
  const [editing, setEditing] = useState<CategoryDraft | null>(null);

  useEffect(() => {
    if (data) setGroups(data.groups);
  }, [data]);

  const save = useTxnMutation(
    (next: CategoryGroup[]) =>
      api.put<{ groups: CategoryGroup[] }>("/categories/layout", {
        groups: next.map((g) => ({ id: g.id, category_ids: g.categories.map((c) => c.id) })),
      }),
    (r) => qc.setQueryData(categoriesQuery.queryKey, r),
  );

  /** Moves a category to index in group to (the same group reorders it) and saves. */
  const move = (id: number, to: number, index: number) => {
    const from = groups.find((g) => g.categories.some((c) => c.id === id));
    const target = groups.find((g) => g.id === to);
    if (!from || !target || (from.id !== to && !(movable.has(from.kind) && movable.has(target.kind)))) return;
    const cat = from.categories.find((c) => c.id === id)!;
    const oldIndex = from.categories.indexOf(cat);
    const next = groups.map((g) => ({ ...g, categories: g.categories.filter((c) => c.id !== id) }));
    const dest = next.find((g) => g.id === to)!;
    // Dropping below itself in the same group: the list shrank by one above the target.
    const at = Math.max(0, Math.min(from.id === to && index > oldIndex ? index - 1 : index, dest.categories.length));
    if (from.id === to && at === oldIndex) return;
    dest.categories.splice(at, 0, cat);
    setGroups(next);
    save.mutate(next);
  };

  const canDrop = (group: CategoryGroup) => {
    if (!drag) return false;
    const from = groups.find((g) => g.id === drag.from);
    return !!from && (from.id === group.id || (movable.has(from.kind) && movable.has(group.kind)));
  };
  const over = (e: DragEvent, group: CategoryGroup, index: number) => {
    if (!canDrop(group)) return;
    e.preventDefault();
    e.stopPropagation();
    if (dropAt?.group !== group.id || dropAt.index !== index) setDropAt({ group: group.id, index });
  };
  const drop = (e: DragEvent) => {
    e.preventDefault();
    if (drag && dropAt) move(drag.id, dropAt.group, dropAt.index);
    setDrag(null);
    setDropAt(null);
  };
  const moveOptions = groups.filter((g) => movable.has(g.kind)).map((g) => ({ value: String(g.id), label: g.name }));

  return (
    <div className="flex flex-col gap-4">
      <p className="text-[13px] text-muted">
        Add your own categories, rename them, or drag them to reorder and move them between Fixed, Flexible and Non-monthly. The budget shows them in this order.
      </p>
      {groups.map((g) => (
        <div key={g.id} data-testid={`category-group-${g.kind}`}>
          <Card
            title={g.name}
            action={
              <Button size="sm" variant="ghost" onClick={() => setEditing({ group: g })} aria-label={`Add category to ${g.name}`}>
                <Plus size={14} /> Add category
              </Button>
            }
          >
            <div className="-m-4">
              {groupHints[g.kind] && <p className="border-b border-border px-4 py-2.5 text-[13px] text-muted">{groupHints[g.kind]}</p>}
              <ul
                className={clsx("divide-y divide-border", drag && canDrop(g) && g.categories.length === 0 && "min-h-12")}
                onDragOver={(e) => over(e, g, g.categories.length)}
                onDrop={drop}
                aria-label={`${g.name} categories`}
              >
                {g.categories.length === 0 && <li className="px-4 py-3 text-[13px] text-muted">No categories. Drag one here.</li>}
                {g.categories.map((c, i) => (
                  <li
                    key={c.id}
                    draggable
                    onDragStart={(e) => {
                      e.dataTransfer.effectAllowed = "move";
                      e.dataTransfer.setData("text/plain", String(c.id));
                      setDrag({ id: c.id, from: g.id });
                    }}
                    onDragEnd={() => {
                      setDrag(null);
                      setDropAt(null);
                    }}
                    onDragOver={(e) => {
                      const r = e.currentTarget.getBoundingClientRect();
                      over(e, g, e.clientY > r.top + r.height / 2 ? i + 1 : i);
                    }}
                    className={clsx(
                      "relative flex items-center gap-2 bg-surface px-2 py-1.5 pr-3 text-sm first:rounded-t-none last:rounded-b-xl",
                      drag?.id === c.id && "opacity-40",
                      dropAt?.group === g.id && dropAt.index === i && "shadow-[inset_0_2px_0_var(--color-accent)]",
                      dropAt?.group === g.id &&
                        dropAt.index === i + 1 &&
                        i === g.categories.length - 1 &&
                        "shadow-[inset_0_-2px_0_var(--color-accent)]",
                    )}
                    data-testid="category-row"
                  >
                    <GripVertical size={16} className="shrink-0 cursor-grab text-muted" aria-hidden />
                    <CategoryIcon icon={c.icon} size="sm" />
                    <span className="min-w-0 flex-1 truncate">{c.name}</span>
                    <Button size="sm" variant="ghost" className="px-2" aria-label={`Edit ${c.name}`} onClick={() => setEditing({ group: g, category: c })}>
                      <Pencil size={13} />
                    </Button>
                    {movable.has(g.kind) && (
                      <Select
                        label={`Group for ${c.name}`}
                        hideLabel
                        className="w-32 shrink-0"
                        value={String(g.id)}
                        onChange={(e) => move(c.id, Number(e.target.value), Number.MAX_SAFE_INTEGER)}
                        options={moveOptions}
                      />
                    )}
                    <Button
                      size="sm"
                      variant="ghost"
                      className="px-2"
                      aria-label={`Move ${c.name} up`}
                      disabled={i === 0}
                      onClick={() => move(c.id, g.id, i - 1)}
                    >
                      <ArrowUp size={14} />
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      className="px-2"
                      aria-label={`Move ${c.name} down`}
                      disabled={i === g.categories.length - 1}
                      onClick={() => move(c.id, g.id, i + 2)}
                    >
                      <ArrowDown size={14} />
                    </Button>
                  </li>
                ))}
              </ul>
            </div>
          </Card>
        </div>
      ))}
      <FormError error={save.error} />
      <CategoryDialog draft={editing} onClose={() => setEditing(null)} />
    </div>
  );
}
