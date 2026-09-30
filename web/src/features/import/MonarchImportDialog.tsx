import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, FileSpreadsheet, Upload } from "lucide-react";
import { useState } from "react";
import { Button, Dialog, Field, FormError, Select } from "@/components/ui";
import { accountLabel, accountsQuery, typeLabels } from "@/features/accounts/api";
import { categoriesQuery } from "@/features/transactions/api";
import { api } from "@/lib/api";
import { formatMoney } from "@/lib/format";

type PreviewAccount = {
  key: string;
  name: string;
  mask: string;
  transactions: number;
  balances: number;
  latest_balance: number;
  latest_date: string;
  type: string;
  account_id: number | null;
};
type PreviewCategory = { name: string; count: number; category_id: number | null; group_id: number; icon: string };
type Preview = {
  transactions: number;
  balances: number;
  from: string;
  to: string;
  already_imported: number;
  accounts: PreviewAccount[];
  categories: PreviewCategory[];
};
type Result = {
  accounts_created: number;
  categories_created: number;
  imported: number;
  matched: number;
  already_imported: number;
  skipped: number;
  balance_snapshots: number;
};

type Files = { transactions: string; balances: string; names: { transactions?: string; balances?: string } };
// Account choice: "new", "skip" or an existing account id; categories: "none", "new:<group>" or an id.
type AcctChoice = { choice: string; name: string; type: string };

const dateLabel = (d: string) => (d ? new Date(d + "T00:00:00").toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" }) : "");

/** Reads the chosen CSVs and tells the two Monarch exports apart by their header row. */
async function readFiles(list: FileList, prev: Files): Promise<Files> {
  const out: Files = { ...prev, names: { ...prev.names } };
  for (const f of Array.from(list)) {
    const text = await f.text();
    const head = text.slice(0, 300).split(/\r?\n/)[0].toLowerCase();
    if (head.includes("merchant")) {
      out.transactions = text;
      out.names.transactions = f.name;
    } else if (head.includes("balance")) {
      out.balances = text;
      out.names.balances = f.name;
    } else {
      throw new Error(`${f.name} isn't a Monarch transactions or balances export.`);
    }
  }
  return out;
}

export function MonarchImportDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const qc = useQueryClient();
  const [files, setFiles] = useState<Files>({ transactions: "", balances: "", names: {} });
  const [fileError, setFileError] = useState<string | null>(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [accts, setAccts] = useState<Record<string, AcctChoice>>({});
  const [cats, setCats] = useState<Record<string, string>>({});
  const [showMatched, setShowMatched] = useState(false);
  const { data: acctData } = useQuery(accountsQuery);
  const { data: catData } = useQuery(categoriesQuery);

  const reset = () => {
    setFiles({ transactions: "", balances: "", names: {} });
    setPreview(null);
    setFileError(null);
    load.reset();
    run.reset();
  };
  const close = (v: boolean) => {
    onOpenChange(v);
    if (!v) setTimeout(reset, 200);
  };

  const load = useMutation({
    mutationFn: () => api.post<Preview>("/import/monarch/preview", { transactions: files.transactions, balances: files.balances }),
    onSuccess: (p) => {
      setPreview(p);
      setAccts(
        Object.fromEntries(
          p.accounts.map((a) => [a.key, { choice: a.account_id ? String(a.account_id) : "new", name: a.name, type: a.type }]),
        ),
      );
      setCats(Object.fromEntries(p.categories.map((c) => [c.name, c.category_id ? String(c.category_id) : c.name ? `new:${c.group_id}` : "none"])));
    },
  });
  const run = useMutation({
    mutationFn: () =>
      api.post<Result>("/import/monarch", {
        transactions: files.transactions,
        balances: files.balances,
        accounts: preview!.accounts.map((a) => {
          const c = accts[a.key];
          if (c.choice === "new") return { key: a.key, create: { name: c.name, type: c.type } };
          if (c.choice === "skip") return { key: a.key };
          return { key: a.key, account_id: Number(c.choice) };
        }),
        categories: preview!.categories.map((c) => {
          const v = cats[c.name];
          if (v.startsWith("new:")) return { name: c.name, create: { group_id: Number(v.slice(4)), icon: c.icon } };
          if (v === "none") return { name: c.name };
          return { name: c.name, category_id: Number(v) };
        }),
      }),
    onSuccess: () => qc.invalidateQueries(),
  });

  const groups = catData?.groups ?? [];
  const catOptions = (name: string) => [
    { value: "none", label: "Leave uncategorized" },
    ...(name ? groups.map((g) => ({ value: `new:${g.id}`, label: `New “${name}” in ${g.name}` })) : []),
    ...groups.flatMap((g) => g.categories.map((c) => ({ value: String(c.id), label: `${c.icon} ${c.name}` }))),
  ];
  const existing = (acctData?.accounts ?? []).filter((a) => a.status !== "closed" && a.status !== "ignored");
  const acctOptions = [
    { value: "new", label: "Create a new account" },
    ...existing.map((a) => ({ value: String(a.id), label: `Add to ${accountLabel(a)}` })),
    { value: "skip", label: "Don't import" },
  ];
  const importing = preview
    ? preview.accounts.filter((a) => accts[a.key]?.choice !== "skip").reduce((n, a) => n + a.transactions, 0) - preview.already_imported
    : 0;
  const matchedCats = preview?.categories.filter((c) => c.category_id) ?? [];
  const otherCats = preview?.categories.filter((c) => !c.category_id) ?? [];

  const step = run.data ? "done" : preview ? "review" : "files";
  const footer =
    step === "files" ? (
      <>
        <Button variant="secondary" onClick={() => close(false)}>
          Cancel
        </Button>
        <Button disabled={!files.transactions && !files.balances} loading={load.isPending} onClick={() => load.mutate()}>
          Continue
        </Button>
      </>
    ) : step === "review" ? (
      <>
        <Button variant="secondary" onClick={() => (setPreview(null), run.reset())} disabled={run.isPending}>
          Back
        </Button>
        <Button loading={run.isPending} onClick={() => run.mutate()}>
          Import {importing > 0 ? `${importing.toLocaleString()} transactions` : "balances"}
        </Button>
      </>
    ) : (
      <Button onClick={() => close(false)}>Done</Button>
    );

  return (
    <Dialog open={open} onOpenChange={close} title="Import from Monarch" className="!max-w-2xl" footer={footer}>
      {step === "files" && (
        <div className="flex flex-col gap-4">
          <p className="text-sm text-muted">
            Export your transactions from Monarch as CSV, plus the account balance history if you have it. Choose one or both files.
            Importing the same export again skips what's already here.
          </p>
          <label className="flex cursor-pointer flex-col items-center gap-2 rounded-xl border border-dashed border-border px-4 py-8 text-center hover:bg-surface-2">
            <Upload size={20} className="text-accent" />
            <span className="text-sm font-medium">Choose CSV files</span>
            <span className="text-xs text-muted">Transactions_….csv and Balances_….csv</span>
            <input
              type="file"
              accept=".csv,text/csv"
              multiple
              className="sr-only"
              aria-label="Monarch CSV files"
              onChange={async (e) => {
                setFileError(null);
                const list = e.target.files;
                if (!list?.length) return;
                try {
                  setFiles(await readFiles(list, files));
                } catch (err) {
                  setFileError(err instanceof Error ? err.message : String(err));
                }
                e.target.value = "";
              }}
            />
          </label>
          <ul className="flex flex-col gap-1.5 text-sm">
            {(["transactions", "balances"] as const).map((k) => (
              <li key={k} className="flex items-center gap-2">
                <FileSpreadsheet size={16} className={files.names[k] ? "text-positive" : "text-muted"} />
                <span className="w-28 text-muted">{k === "transactions" ? "Transactions" : "Balances"}</span>
                <span className={files.names[k] ? "truncate" : "text-muted"}>{files.names[k] ?? "not chosen"}</span>
              </li>
            ))}
          </ul>
          {fileError && <p className="text-[13px] text-negative">{fileError}</p>}
          <FormError error={load.error} />
        </div>
      )}

      {step === "review" && preview && (
        <div className="flex flex-col gap-5">
          <p className="text-sm">
            {preview.transactions > 0 && (
              <>
                <span className="font-medium">{preview.transactions.toLocaleString()} transactions</span> from {dateLabel(preview.from)} to{" "}
                {dateLabel(preview.to)}
              </>
            )}
            {preview.transactions > 0 && preview.balances > 0 && " · "}
            {preview.balances > 0 && <span>{preview.balances.toLocaleString()} balance entries</span>}
            {preview.already_imported > 0 && (
              <span className="block text-[13px] text-muted">{preview.already_imported.toLocaleString()} were imported before and will be skipped.</span>
            )}
          </p>

          <section className="flex flex-col gap-2">
            <div>
              <h3 className="text-[13px] font-medium">Accounts</h3>
              <p className="text-xs text-muted">
                Add to an existing account to fill in its history: transactions already there (for example from SimpleFIN) get Monarch's category and
                notes instead of a duplicate.
              </p>
            </div>
            <ul className="divide-y divide-border rounded-lg border border-border">
              {preview.accounts.map((a) => {
                const c = accts[a.key];
                const set = (patch: Partial<AcctChoice>) => setAccts({ ...accts, [a.key]: { ...c, ...patch } });
                return (
                  <li key={a.key} className="flex flex-col gap-2 px-3 py-2.5" data-testid="import-account">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <div className="min-w-0">
                        <div className="truncate text-sm font-medium">{a.key}</div>
                        <div className="text-xs text-muted">
                          {a.transactions > 0 ? `${a.transactions} transactions` : "Balance history only"} · {formatMoney(a.latest_balance)}
                        </div>
                      </div>
                      <Select label={`Import ${a.key} into`} hideLabel value={c.choice} onChange={(e) => set({ choice: e.target.value })} options={acctOptions} className="w-60" />
                    </div>
                    {c.choice === "new" && (
                      <div className="flex flex-wrap gap-2">
                        <Field label={`Name for ${a.key}`} hideLabel value={c.name} onChange={(e) => set({ name: e.target.value })} className="min-w-40 flex-1" />
                        <Select
                          label={`Type for ${a.key}`}
                          hideLabel
                          value={c.type}
                          onChange={(e) => set({ type: e.target.value })}
                          options={Object.entries(typeLabels).map(([value, label]) => ({ value, label }))}
                          className="w-40"
                        />
                      </div>
                    )}
                  </li>
                );
              })}
            </ul>
          </section>

          {preview.categories.length > 0 && (
            <section className="flex flex-col gap-2">
              <div>
                <h3 className="text-[13px] font-medium">Categories</h3>
                <p className="text-xs text-muted">
                  {matchedCats.length} match a Viceroy category by name.{" "}
                  <button className="font-medium text-accent hover:underline" onClick={() => setShowMatched(!showMatched)}>
                    {showMatched ? "Hide them" : "Show them"}
                  </button>
                </p>
              </div>
              <ul className="divide-y divide-border rounded-lg border border-border">
                {[...otherCats, ...(showMatched ? matchedCats : [])].map((c) => (
                  <li key={c.name} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2" data-testid="import-category">
                    <div className="text-sm">
                      <span className="font-medium">{c.name || "Uncategorized"}</span>
                      <span className="text-muted"> · {c.count}</span>
                    </div>
                    <Select
                      label={`Category for ${c.name || "Uncategorized"}`}
                      hideLabel
                      value={cats[c.name]}
                      onChange={(e) => setCats({ ...cats, [c.name]: e.target.value })}
                      options={catOptions(c.name)}
                      className="w-60"
                    />
                  </li>
                ))}
              </ul>
            </section>
          )}
          <FormError error={run.error} />
        </div>
      )}

      {step === "done" && run.data && (
        <div className="flex flex-col items-center gap-3 py-6 text-center" role="status">
          <CheckCircle2 size={32} className="text-positive" />
          <h3 className="text-[15px] font-semibold">Import finished</h3>
          <ul className="text-sm text-muted">
            <li>{run.data.imported.toLocaleString()} transactions imported</li>
            {run.data.matched > 0 && <li>{run.data.matched.toLocaleString()} matched transactions already here (details copied)</li>}
            {run.data.already_imported > 0 && <li>{run.data.already_imported.toLocaleString()} skipped as already imported</li>}
            {run.data.accounts_created > 0 && <li>{run.data.accounts_created} accounts created</li>}
            {run.data.categories_created > 0 && <li>{run.data.categories_created} categories created</li>}
            {run.data.balance_snapshots > 0 && <li>{run.data.balance_snapshots.toLocaleString()} days of balance history</li>}
          </ul>
        </div>
      )}
    </Dialog>
  );
}
