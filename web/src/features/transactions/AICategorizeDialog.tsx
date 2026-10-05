import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { Button, Dialog, Field, FormError } from "@/components/ui";
import { ApiError, api } from "@/lib/api";
import { useTxnMutation } from "./api";

type Result = { merchants: number; categorized: number; skipped: number };

/** Transactions > ⋯ > Categorize with AI: the light AI model sorts uncategorized transactions. */
export function AICategorizeDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const [days, setDays] = useState("31");
  const [result, setResult] = useState<Result | null>(null);
  const run = useTxnMutation(() => api.post<Result>("/transactions/ai-categorize", { days: Number(days) }), setResult);
  const notSetUp = run.error instanceof ApiError && run.error.message.includes("Settings → AI");
  const close = (o: boolean) => {
    onOpenChange(o);
    if (!o) {
      setResult(null);
      run.reset();
    }
  };
  return (
    <Dialog
      open={open}
      onOpenChange={close}
      title="Categorize with AI"
      description="The AI looks at each merchant once and picks a category. Its picks are marked for review, and later transactions from the same merchant follow them without asking the AI again."
      footer={
        result ? (
          <Button size="sm" onClick={() => close(false)}>
            Done
          </Button>
        ) : (
          <>
            <Button variant="ghost" size="sm" onClick={() => close(false)}>
              Cancel
            </Button>
            <Button size="sm" type="submit" form="ai-categorize-form" loading={run.isPending}>
              Categorize
            </Button>
          </>
        )
      }
    >
      {result ? (
        <p className="text-sm" data-testid="ai-categorize-result">
          {result.merchants === 0
            ? "Nothing uncategorized in that range."
            : `Categorized ${result.categorized} ${result.categorized === 1 ? "transaction" : "transactions"} from ${result.merchants} ${result.merchants === 1 ? "merchant" : "merchants"}.` +
              (result.skipped > 0 ? ` ${result.skipped} the AI wasn't sure about are still uncategorized.` : "") +
              (result.categorized > 0 ? " Check them under Needs review." : "")}
        </p>
      ) : (
        <form
          id="ai-categorize-form"
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            run.mutate(undefined);
          }}
        >
          <Field
            label="Uncategorized transactions from the last"
            type="number"
            min={1}
            max={365}
            value={days}
            onChange={(e) => setDays(e.target.value)}
            hint="Days. New transactions are categorized automatically when AI is on."
            required
          />
          <FormError error={run.error} />
          {notSetUp && (
            <Link to={"/settings" as string} hash="ai" className="text-[13px] text-accent hover:underline" onClick={() => close(false)}>
              Open Settings → AI
            </Link>
          )}
        </form>
      )}
    </Dialog>
  );
}
