import { useQuery } from "@tanstack/react-query";
import { Plus, Target, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Button, Card, Dialog, EmptyState, Field, FormError, MoneyText, PageHeader, Switch } from "@/components/ui";
import { centsToInput, useBudgetMutation } from "@/features/budget/api";
import { api } from "@/lib/api";
import { goalsQuery, type Goal } from "./api";

const fmtDate = (d: string) => new Date(d + "T00:00:00").toLocaleDateString("en-US", { month: "short", year: "numeric" });

export function GoalsPage() {
  const { data } = useQuery(goalsQuery);
  const [editing, setEditing] = useState<Goal | null>(null);
  const [open, setOpen] = useState(false);
  const goals = data?.goals ?? [];
  const active = goals.filter((g) => !g.archived);
  const archived = goals.filter((g) => g.archived);
  const edit = (g: Goal | null) => {
    setEditing(g);
    setOpen(true);
  };

  return (
    <>
      <PageHeader
        title="Goals"
        actions={
          <Button size="sm" onClick={() => edit(null)}>
            <Plus size={15} /> Add goal
          </Button>
        }
      />
      <div className="mx-auto flex max-w-4xl flex-col gap-4 p-4 md:p-6">
        {data && goals.length === 0 ? (
          <Card>
            <EmptyState icon={Target} title="No goals yet">
              Save toward a trip, an emergency fund or a big purchase. Budget a monthly contribution on the Budget tab and assign transfers to the goal from a transaction.
            </EmptyState>
          </Card>
        ) : (
          <div className="grid gap-4 sm:grid-cols-2">
            {active.map((g) => (
              <GoalCard key={g.id} g={g} onClick={() => edit(g)} />
            ))}
          </div>
        )}
        {archived.length > 0 && (
          <>
            <h2 className="mt-2 text-[13px] font-medium text-muted">Archived</h2>
            <div className="grid gap-4 opacity-70 sm:grid-cols-2">
              {archived.map((g) => (
                <GoalCard key={g.id} g={g} onClick={() => edit(g)} />
              ))}
            </div>
          </>
        )}
      </div>
      <GoalDialog open={open} onOpenChange={setOpen} goal={editing} />
    </>
  );
}

function GoalCard({ g, onClick }: { g: Goal; onClick: () => void }) {
  const pct = g.target_cents > 0 ? Math.min(100, (g.balance_cents / g.target_cents) * 100) : 0;
  return (
    <button onClick={onClick} className="rounded-xl border border-border bg-surface p-4 text-left hover:bg-surface-2" data-testid="goal-card">
      <div className="flex items-center gap-3">
        <span className="grid size-10 place-items-center rounded-full bg-accent-soft text-lg" aria-hidden>
          {g.icon}
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate text-[15px] font-semibold">{g.name}</div>
          <div className="text-[13px] text-muted">{g.target_date ? `By ${fmtDate(g.target_date)}` : "No target date"}</div>
        </div>
      </div>
      <div className="mt-4 flex items-baseline justify-between text-[13px]">
        <MoneyText cents={g.balance_cents} className="text-base font-semibold" />
        {g.target_cents > 0 && (
          <span className="text-muted">
            of <MoneyText cents={g.target_cents} whole />
          </span>
        )}
      </div>
      <div className="mt-1.5 h-2 rounded-full bg-surface-2" aria-hidden>
        <div className="h-full rounded-full bg-accent" style={{ width: `${pct}%` }} />
      </div>
      {g.target_cents > 0 && <div className="mt-1 text-xs text-muted">{Math.floor(pct)}% there</div>}
    </button>
  );
}

function GoalDialog({ open, onOpenChange, goal }: { open: boolean; onOpenChange: (o: boolean) => void; goal: Goal | null }) {
  const [name, setName] = useState("");
  const [icon, setIcon] = useState("");
  const [target, setTarget] = useState("");
  const [targetDate, setTargetDate] = useState("");
  const [starting, setStarting] = useState("");
  const [archivedOn, setArchived] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => {
    if (!open) return;
    setName(goal?.name ?? "");
    setIcon(goal?.icon ?? "🎯");
    setTarget(goal?.target_cents ? centsToInput(goal.target_cents) : "");
    setTargetDate(goal?.target_date ?? "");
    setStarting(goal?.starting_cents ? centsToInput(goal.starting_cents) : "");
    setArchived(goal?.archived ?? false);
    setConfirmDelete(false);
  }, [open, goal]);

  const close = () => onOpenChange(false);
  const save = useBudgetMutation(() => {
    const body = { name, icon, target, target_date: targetDate, starting, archived: archivedOn };
    return goal ? api.patch(`/goals/${goal.id}`, body) : api.post("/goals", body);
  }, close);
  const del = useBudgetMutation(() => api.del(`/goals/${goal!.id}`), close);
  useEffect(() => {
    if (!open) {
      save.reset();
      del.reset();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={goal ? "Edit goal" : "New goal"}
      footer={
        <>
          {goal &&
            (confirmDelete ? (
              <Button variant="danger" className="mr-auto" loading={del.isPending} onClick={() => del.mutate(undefined)}>
                Delete goal
              </Button>
            ) : (
              <Button variant="danger-ghost" className="mr-auto" aria-label="Delete goal" onClick={() => setConfirmDelete(true)}>
                <Trash2 size={14} />
              </Button>
            ))}
          <Button variant="secondary" onClick={close}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate(undefined)} loading={save.isPending}>
            Save
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate(undefined);
        }}
      >
        <div className="grid grid-cols-[4rem_1fr] gap-3">
          <Field label="Icon" value={icon} onChange={(e) => setIcon(e.target.value)} maxLength={8} className="text-center" />
          <Field label="Name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Vacation" autoFocus />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Target amount" inputMode="decimal" value={target} onChange={(e) => setTarget(e.target.value)} placeholder="5000" />
          <Field label="Target date" type="date" value={targetDate} onChange={(e) => setTargetDate(e.target.value)} />
        </div>
        <Field label="Already saved" inputMode="decimal" value={starting} onChange={(e) => setStarting(e.target.value)} placeholder="0" hint="Money set aside before you started tracking." />
        {goal && <Switch label="Archived" hint="Hides it from the budget; history is kept." checked={archivedOn} onCheckedChange={setArchived} />}
        {confirmDelete && <p className="text-[13px] text-negative">Deleting removes the goal and its monthly budget. Transactions stay, unassigned.</p>}
        <FormError error={save.error ?? del.error} />
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}
