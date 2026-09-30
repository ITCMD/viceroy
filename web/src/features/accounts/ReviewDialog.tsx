import { useState } from "react";
import { Button, Dialog, FormError, MoneyText, Select } from "@/components/ui";
import { api } from "@/lib/api";
import { accountSubtitle, useAccountsMutation, type Account } from "./api";

/** Resolves accounts that sync could not confidently match after a reconnect. */
export function ReviewDialog({ open, onOpenChange, accounts }: { open: boolean; onOpenChange: (o: boolean) => void; accounts: Account[] }) {
  const review = accounts.filter((a) => a.status === "review");
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Review linked accounts"
      description="These came back from your bank but look like accounts you already have. Link each to the existing account to keep its history, or keep it as a new one."
      footer={<Button variant="secondary" onClick={() => onOpenChange(false)}>Close</Button>}
    >
      {review.length === 0 ? (
        <p className="text-sm text-muted">All accounts are reviewed.</p>
      ) : (
        <div className="flex flex-col divide-y divide-border">
          {review.map((a) => (
            <ReviewRow key={a.id} account={a} all={accounts} />
          ))}
        </div>
      )}
    </Dialog>
  );
}

function ReviewRow({ account, all }: { account: Account; all: Account[] }) {
  const targets = all.filter((t) => t.id !== account.id && t.status !== "review" && t.status !== "ignored");
  // Likely matches (same institution) first.
  targets.sort((x, y) => Number(y.institution_name === account.institution_name) - Number(x.institution_name === account.institution_name));
  const [target, setTarget] = useState(String(account.review_candidate_id ?? targets[0]?.id ?? ""));
  const resolve = useAccountsMutation((body: { action: string; target_id?: number }) => api.post(`/accounts/${account.id}/resolve`, body));

  return (
    <div className="flex flex-col gap-3 py-3 first:pt-0 last:pb-0" data-testid="review-row">
      <div className="flex items-baseline justify-between gap-3">
        <div>
          <div className="text-sm font-medium">{account.name}</div>
          <div className="text-xs text-muted">{accountSubtitle(account)}</div>
        </div>
        <MoneyText cents={account.balance_cents} className="text-sm" />
      </div>
      {targets.length > 0 && (
        <Select
          label="Existing account"
          value={target}
          onChange={(e) => setTarget(e.target.value)}
          options={targets.map((t) => ({
            value: String(t.id),
            label: `${t.name} (${accountSubtitle(t)}${t.status === "disconnected" ? ", disconnected" : ""})`,
          }))}
        />
      )}
      <FormError error={resolve.error} />
      <div className="flex flex-wrap gap-2">
        {targets.length > 0 && (
          <Button size="sm" loading={resolve.isPending} onClick={() => resolve.mutate({ action: "link", target_id: Number(target) })}>
            Link to existing
          </Button>
        )}
        <Button size="sm" variant="secondary" disabled={resolve.isPending} onClick={() => resolve.mutate({ action: "keep" })}>
          Keep as new
        </Button>
        <Button size="sm" variant="ghost" disabled={resolve.isPending} onClick={() => resolve.mutate({ action: "ignore" })}>
          Ignore
        </Button>
      </div>
    </div>
  );
}
