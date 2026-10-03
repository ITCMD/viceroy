import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CheckCircle2, FileSpreadsheet, ImagePlus, Sparkles, Upload, X } from "lucide-react";
import { useState, type ClipboardEvent } from "react";
import { Badge, Button, CategoryIcon, Dialog, Field, FormError, Select, Tabs, TextArea, withIcon } from "@/components/ui";
import { aiSettingsQuery } from "@/features/settings/ai";
import { api } from "@/lib/api";
import { shrinkImage } from "@/lib/image";
import { formatMoney } from "@/lib/format";
import { budgetQuery, centsToInput, chunkLabel, monthLabel, useBudgetMutation, type Chunk } from "./api";

type Source = "csv" | "text" | "image";

type PreviewRow = {
  group: string;
  category: string;
  /** AI: the name as written, when it was matched to a differently named category. */
  source?: string;
  icon: string;
  amount: number;
  timing: Chunk | null;
  target: string; // cat:<id> | goal:<id> | new:<group id> | new:goals | skip
  status: "new" | "changed" | "same" | "skip";
  old_amount: number;
  old_timing: Chunk | null;
  note?: string;
};
type Preview = { month: string; source: "csv" | "ai"; model: string; rows: PreviewRow[]; problems: string[] | null };
type Result = { created: number; updated: number; unchanged: number };
type Row = PreviewRow & { input: string };

const MAX_IMAGES = 4;

/** The name a new category gets: the budget's own wording when the AI matched it to an
 * existing category (so it can be created instead), else the row's name. */
const newName = (r: PreviewRow) => r.source || r.category;
const sources: { value: Source; label: string }[] = [
  { value: "csv", label: "CSV file" },
  { value: "text", label: "Paste text" },
  { value: "image", label: "Screenshot" },
];

const toCents = (s: string) => {
  const v = Number(s.replace(/[$,\s]/g, ""));
  return s.trim() === "" ? 0 : Number.isFinite(v) && v >= 0 ? Math.round(v * 100) : null;
};

const sameTiming = (a: Chunk | null, b: Chunk | null) => !a || !b || JSON.stringify(a) === JSON.stringify(b);

/**
 * Import a budget setup (categories, monthly amounts, timing) from a CSV file, or have the
 * multimodal AI read it from pasted text or screenshots. Rows are matched to existing
 * categories and can be re-pointed before anything is saved.
 */
export function BudgetImportDialog({ open, onOpenChange, month }: { open: boolean; onOpenChange: (v: boolean) => void; month: string }) {
  const qc = useQueryClient();
  const { data: budget } = useQuery({ ...budgetQuery("month", month ? `${month}-01` : ""), enabled: open && !!month });
  const { data: ai } = useQuery({ ...aiSettingsQuery, enabled: open });
  const [source, setSource] = useState<Source>("csv");
  const [csv, setCsv] = useState<{ name: string; text: string } | null>(null);
  const [text, setText] = useState("");
  const [images, setImages] = useState<string[]>([]);
  const [fileError, setFileError] = useState<string | null>(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [rows, setRows] = useState<Row[]>([]);

  const load = useBudgetMutation(
    () =>
      api.post<Preview>(
        "/budget/import/preview",
        source === "csv" ? { month, csv: csv?.text ?? "" } : source === "text" ? { month, text } : { month, images },
      ),
    (p) => {
      setPreview(p);
      setRows(p.rows.map((r) => ({ ...r, input: centsToInput(r.amount) })));
    },
  );
  const run = useBudgetMutation(
    () =>
      api.post<Result>("/budget/import", {
        month,
        rows: rows
          .filter((r) => r.target !== "skip")
          .map((r) => ({
            target: r.target,
            category: r.target.startsWith("new:") ? newName(r) : r.category,
            icon: r.icon,
            amount: toCents(r.input) ?? 0,
            timing: r.timing,
          })),
      }),
    () => qc.invalidateQueries({ queryKey: ["categories"] }),
  );

  const reset = () => {
    setCsv(null);
    setText("");
    setImages([]);
    setFileError(null);
    setPreview(null);
    load.reset();
    run.reset();
  };
  const close = (v: boolean) => {
    onOpenChange(v);
    if (!v) setTimeout(reset, 200);
  };

  const addImages = async (files: File[]) => {
    setFileError(null);
    const imgs = files.filter((f) => f.type.startsWith("image/"));
    if (images.length + imgs.length > MAX_IMAGES) setFileError(`Up to ${MAX_IMAGES} screenshots.`);
    try {
      const added = await Promise.all(imgs.slice(0, MAX_IMAGES - images.length).map((f) => shrinkImage(f, 2000)));
      setImages((prev) => [...prev, ...added].slice(0, MAX_IMAGES));
    } catch {
      setFileError("Couldn't read that image.");
    }
  };
  const onPaste = (e: ClipboardEvent) => {
    const files = Array.from(e.clipboardData.files);
    if (step === "source" && files.some((f) => f.type.startsWith("image/"))) {
      e.preventDefault();
      setSource("image");
      addImages(files);
    }
  };

  // Existing budget lines by target, to show what a row changes.
  const lines = new Map<string, { name: string; icon: string; amount: number; chunk: Chunk | null }>();
  for (const g of budget?.groups ?? [])
    for (const l of g.lines)
      lines.set(`${g.kind === "goals" ? "goal" : "cat"}:${l.id}`, { name: l.name, icon: l.icon, amount: l.month_budget, chunk: g.kind === "goals" ? null : l.chunk });
  const targetOptions = (r: Row) => [
    { value: "skip", label: "Don't import" },
    ...(budget?.groups ?? []).map((g) =>
      g.kind === "goals" ? { value: "new:goals", label: `New goal “${newName(r)}”` } : { value: `new:${g.id}`, label: `New “${newName(r)}” in ${g.name}` },
    ),
    ...(budget?.groups ?? []).flatMap((g) =>
      g.lines.map((l) => ({ value: `${g.kind === "goals" ? "goal" : "cat"}:${l.id}`, label: `${g.name} · ${withIcon(l.icon, l.name)}` })),
    ),
  ];
  const status = (r: Row): { tone: "neutral" | "accent" | "positive"; label: string } => {
    if (r.target === "skip") return { tone: "neutral", label: "Skipped" };
    const old = lines.get(r.target);
    if (!old) return { tone: "positive", label: "New" };
    const cents = toCents(r.input);
    if (cents === old.amount && sameTiming(r.timing, old.chunk)) return { tone: "neutral", label: "No change" };
    return { tone: "accent", label: cents === old.amount ? "New timing" : `Was ${formatMoney(old.amount)}` };
  };
  const badAmount = rows.some((r) => r.target !== "skip" && toCents(r.input) === null);
  const changes = rows.filter((r) => r.target !== "skip" && status(r).label !== "No change").length;
  const set = (i: number, patch: Partial<Row>) => setRows(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));

  const needsAI = source !== "csv";
  const ready = source === "csv" ? !!csv : source === "text" ? text.trim() !== "" : images.length > 0;
  const step = run.data ? "done" : preview ? "review" : "source";
  const exportURL = `/api/budget/export?month=${month}`;

  const footer =
    step === "source" ? (
      <>
        <Button variant="secondary" onClick={() => close(false)}>
          Cancel
        </Button>
        <Button disabled={!ready || !month || (needsAI && !ai?.vision_ready)} loading={load.isPending} onClick={() => load.mutate(undefined)}>
          {needsAI ? (load.isPending ? "Reading…" : "Read with AI") : "Continue"}
        </Button>
      </>
    ) : step === "review" ? (
      <>
        <Button variant="secondary" onClick={() => (setPreview(null), run.reset())} disabled={run.isPending}>
          Back
        </Button>
        <Button disabled={changes === 0 || badAmount} loading={run.isPending} onClick={() => run.mutate(undefined)}>
          {changes === 0 ? "Nothing to change" : `Apply ${changes} ${changes === 1 ? "change" : "changes"}`}
        </Button>
      </>
    ) : (
      <Button onClick={() => close(false)}>Done</Button>
    );

  return (
    <Dialog open={open} onOpenChange={close} title="Import budget" className="!max-w-2xl" footer={footer}>
      <div onPaste={onPaste}>
        {step === "source" && (
          <div className="flex flex-col gap-4">
            <Tabs value={source} onChange={setSource} items={sources} />

            {source === "csv" && (
              <>
                <label className="flex cursor-pointer flex-col items-center gap-2 rounded-xl border border-dashed border-border px-4 py-8 text-center hover:bg-surface-2">
                  {csv ? <FileSpreadsheet size={20} className="text-positive" /> : <Upload size={20} className="text-accent" />}
                  <span className="text-sm font-medium">{csv ? csv.name : "Choose a CSV file"}</span>
                  <span className="text-xs text-muted">Columns: Group, Category, Amount (monthly), Timing, Icon</span>
                  <input
                    type="file"
                    accept=".csv,text/csv"
                    className="sr-only"
                    aria-label="Budget CSV file"
                    onChange={async (e) => {
                      const f = e.target.files?.[0];
                      if (f) setCsv({ name: f.name, text: await f.text() });
                      e.target.value = "";
                    }}
                  />
                </label>
                <p className="text-[13px] text-muted">
                  Start from{" "}
                  <a href={exportURL} download className="font-medium text-accent hover:underline">
                    your current budget as a CSV
                  </a>
                  , edit it in a spreadsheet and bring it back. Timing is Evenly, Day 15, Week 2 or Every 2 weeks; leave it empty to keep the current one.
                </p>
              </>
            )}

            {source === "text" && (
              <TextArea
                label="Your budget"
                rows={8}
                value={text}
                onChange={(e) => setText(e.target.value)}
                placeholder={"Rent $1,850 on the 1st\nGroceries $600\nDining out $300\nVacation $2,400 a year"}
                hint="Paste it from anywhere: a spreadsheet, a note, another budgeting app. Weekly and yearly amounts are converted to monthly."
              />
            )}

            {source === "image" && (
              <>
                <label className="flex cursor-pointer flex-col items-center gap-2 rounded-xl border border-dashed border-border px-4 py-8 text-center hover:bg-surface-2">
                  <ImagePlus size={20} className="text-accent" />
                  <span className="text-sm font-medium">Choose or paste screenshots</span>
                  <span className="text-xs text-muted">Of a budget in Monarch, YNAB, a spreadsheet… up to {MAX_IMAGES}</span>
                  <input
                    type="file"
                    accept="image/png,image/jpeg,image/webp,image/gif"
                    multiple
                    className="sr-only"
                    aria-label="Budget screenshots"
                    onChange={(e) => {
                      addImages(Array.from(e.target.files ?? []));
                      e.target.value = "";
                    }}
                  />
                </label>
                {images.length > 0 && (
                  <ul className="flex flex-wrap gap-2" aria-label="Screenshots">
                    {images.map((src, i) => (
                      <li key={i} className="relative">
                        <img src={src} alt={`Screenshot ${i + 1}`} className="h-24 w-auto rounded-lg border border-border object-cover" />
                        <button
                          className="absolute -right-1.5 -top-1.5 rounded-full border border-border bg-surface p-0.5 text-muted hover:text-text"
                          aria-label={`Remove screenshot ${i + 1}`}
                          onClick={() => setImages(images.filter((_, j) => j !== i))}
                        >
                          <X size={12} />
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </>
            )}

            {needsAI && ai && (
              <p className="flex items-start gap-2 text-[13px] text-muted">
                <Sparkles size={14} className="mt-0.5 shrink-0 text-accent" />
                {ai.vision_ready ? (
                  <span>
                    Read by {ai.vision_model || ai.chat_model}. It only sees what you give it here and your category names, and nothing changes until you review the
                    result.
                  </span>
                ) : (
                  <span>
                    Reading budgets with AI needs an OpenRouter key and a multimodal model.{" "}
                    <Link to={"/settings" as string} hash="ai" className="font-medium text-accent hover:underline">
                      Set it up in Settings
                    </Link>
                  </span>
                )}
              </p>
            )}
            {fileError && <p className="text-[13px] text-negative">{fileError}</p>}
            <FormError error={load.error} />
          </div>
        )}

        {step === "review" && preview && (
          <div className="flex flex-col gap-4">
            <p className="text-sm">
              <span className="font-medium">
                {rows.length} budget {rows.length === 1 ? "line" : "lines"}
              </span>
              {preview.source === "ai" && <span className="text-muted"> read by {preview.model}</span>}.{" "}
              <span className="text-muted">Amounts apply from {monthLabel(month)} onward; categories not listed stay as they are.</span>
            </p>
            {!!preview.problems?.length && (
              <ul className="flex flex-col gap-0.5 rounded-lg bg-accent-soft px-3 py-2 text-[13px]" data-testid="import-problems">
                {preview.problems.map((p, i) => (
                  <li key={i}>{p}</li>
                ))}
              </ul>
            )}
            <ul className="divide-y divide-border rounded-lg border border-border">
              {rows.map((r, i) => {
                const st = status(r);
                const existing = lines.get(r.target);
                const timing = r.timing && chunkLabel(r.timing);
                return (
                  <li key={i} className="flex flex-wrap items-center gap-x-3 gap-y-2 px-3 py-2.5" data-testid="import-row">
                    <div className="flex min-w-40 flex-1 items-center gap-2.5">
                      <CategoryIcon icon={existing?.icon || r.icon || "📦"} size="sm" />
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="truncate text-sm font-medium">{r.target.startsWith("new:") ? newName(r) : r.category}</span>
                          <span className="shrink-0 whitespace-nowrap">
                            <Badge tone={st.tone}>{st.label}</Badge>
                          </span>
                        </div>
                        <div className="truncate text-xs text-muted">{[r.group, timing, r.note].filter(Boolean).join(" · ")}</div>
                      </div>
                    </div>
                    <Select
                      label={`Import ${r.category} as`}
                      hideLabel
                      value={r.target}
                      onChange={(e) => set(i, { target: e.target.value })}
                      options={targetOptions(r)}
                      className="w-full sm:w-52"
                    />
                    <Field
                      label={`Monthly amount for ${r.category}`}
                      hideLabel
                      inputMode="decimal"
                      value={r.input}
                      onChange={(e) => set(i, { input: e.target.value })}
                      disabled={r.target === "skip"}
                      aria-invalid={toCents(r.input) === null}
                      className="w-24"
                    />
                  </li>
                );
              })}
            </ul>
            {badAmount && <p className="text-[13px] text-negative">Fix the amounts that aren't numbers.</p>}
            <FormError error={run.error} />
          </div>
        )}

        {step === "done" && run.data && (
          <div className="flex flex-col items-center gap-3 py-6 text-center" role="status">
            <CheckCircle2 size={32} className="text-positive" />
            <h3 className="text-[15px] font-semibold">Budget updated</h3>
            <ul className="text-sm text-muted">
              {run.data.updated > 0 && <li>{run.data.updated} budget lines changed</li>}
              {run.data.created > 0 && <li>{run.data.created} new categories or goals</li>}
              {run.data.unchanged > 0 && <li>{run.data.unchanged} already matched</li>}
            </ul>
          </div>
        )}
      </div>
    </Dialog>
  );
}
