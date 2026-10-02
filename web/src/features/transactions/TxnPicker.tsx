import { useQuery } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { useState } from "react";
import { MoneyText } from "@/components/ui";
import { api } from "@/lib/api";
import { shortDate, type Transaction } from "./api";

/** Transactions up to a week after `around` (newest first), searchable. `outOnly` lists money out only. */
export function TxnPicker({ around, onPick, outOnly }: { around: string; onPick: (t: Transaction) => void; outOnly?: boolean }) {
  const [q, setQ] = useState("");
  const cursor = around ? new Date(Date.parse(around) + 8 * 86_400_000).toISOString().slice(0, 10) + "|0" : "";
  const params = new URLSearchParams({ limit: "40" });
  if (q.trim()) params.set("q", q.trim());
  else if (cursor) params.set("cursor", cursor);
  const { data, isFetching } = useQuery({
    queryKey: ["transactions", "pick", params.toString()],
    queryFn: () => api.get<{ transactions: Transaction[] }>(`/transactions?${params}`),
  });
  return (
    <div className="rounded-lg border border-border">
      <div className="flex items-center gap-2 border-b border-border px-3">
        <Search size={14} className="text-muted" />
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search transactions"
          aria-label="Search transactions"
          className="h-9 flex-1 bg-transparent text-sm outline-none placeholder:text-muted"
        />
      </div>
      <ul className="max-h-56 overflow-y-auto py-1" data-testid="note-txn-list">
        {(data?.transactions ?? []).filter((t) => !outOnly || t.amount_cents < 0).map((t) => (
          <li key={t.id}>
            <button type="button" className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-surface-2" onClick={() => onPick(t)}>
              <span className="w-14 shrink-0 text-xs text-muted">{shortDate(t.date)}</span>
              <span className="min-w-0 flex-1 truncate">{t.merchant}</span>
              <MoneyText cents={t.amount_cents} colored />
            </button>
          </li>
        ))}
        {data && data.transactions.length === 0 && !isFetching && <li className="px-3 py-3 text-center text-[13px] text-muted">No transactions found.</li>}
      </ul>
    </div>
  );
}
