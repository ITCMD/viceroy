import { useMutation } from "@tanstack/react-query";
import { ImagePlus, Loader2, Trash2, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Button, Dialog, Field, FormError, TextArea } from "@/components/ui";
import { centsToInput } from "@/features/budget/api";
import { api } from "@/lib/api";
import { shrinkImage } from "@/lib/image";
import { useWishMutation, type Member, type Preview, type WishItem } from "./api";
import { StarPicker } from "./StarPicker";

/** The picture being edited: unchanged, a link from the page preview, an upload, or removed. */
type Img = { kind: "keep"; src: string } | { kind: "url"; src: string } | { kind: "data"; src: string } | { kind: "none" };

export function WishItemDialog({
  draft,
  members,
  me,
  onClose,
}: {
  draft: { item: WishItem | null; url?: string } | null;
  members: Member[];
  me: number;
  onClose: () => void;
}) {
  const item = draft?.item ?? null;
  const [url, setUrl] = useState("");
  const [title, setTitle] = useState("");
  const [price, setPrice] = useState("");
  const [fetchedPrice, setFetchedPrice] = useState<string | null>(null);
  const [img, setImg] = useState<Img>({ kind: "none" });
  const [stars, setStars] = useState(3);
  const [wanted, setWanted] = useState<number[]>([]);
  const [saves, setSaves] = useState(false);
  const [notes, setNotes] = useState("");
  const [note, setNote] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const lastFetched = useRef("");

  const preview = useMutation({
    mutationFn: (u: string) => api.post<Preview>("/wishlist/preview", { url: u }),
    onSuccess: (p) => {
      setUrl(p.url);
      lastFetched.current = p.url;
      setTitle((t) => t || p.title);
      if (p.price_cents !== null) {
        const v = centsToInput(p.price_cents);
        setPrice((cur) => cur || v);
        setFetchedPrice(v);
      }
      if (p.image_url) setImg((cur) => (cur.kind === "none" ? { kind: "url", src: p.image_url } : cur));
      setNote(
        p.blocked
          ? p.price_cents === null
            ? `${p.store || "The store"} didn't share the price. Fill in what's missing.`
            : `${p.store || "The store"} didn't share everything. Check the details.`
          : "",
      );
    },
  });

  useEffect(() => {
    if (!draft) return;
    setUrl(item?.url ?? draft.url ?? "");
    setTitle(item?.title ?? "");
    setPrice(item?.price_cents != null ? centsToInput(item.price_cents) : "");
    setFetchedPrice(item?.price_source === "fetched" && item.price_cents != null ? centsToInput(item.price_cents) : null);
    setImg(item?.image_url ? { kind: "keep", src: item.image_url } : { kind: "none" });
    setStars(item?.stars ?? 3);
    setWanted(item?.wanted_by ?? (me ? [me] : []));
    setSaves(item?.saves_money ?? false);
    setNotes(item?.notes ?? "");
    setNote("");
    setConfirmDelete(false);
    lastFetched.current = item?.url ?? "";
    preview.reset();
    if (!item && draft.url) fetchLink(draft.url);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft]);

  const fetchLink = (u: string) => {
    const v = u.trim();
    if (!v || v === lastFetched.current) return;
    lastFetched.current = v;
    preview.mutate(v);
  };

  const save = useWishMutation(async () => {
    const body: Record<string, unknown> = {
      title,
      url,
      price,
      price_fetched: fetchedPrice !== null && price.trim() === fetchedPrice,
      stars,
      saves_money: saves,
      notes,
      wanted_by: wanted,
    };
    if (img.kind === "url") body.image_url = img.src;
    if (img.kind === "data") body.image = img.src;
    if (item) {
      await api.patch(`/wishlist/${item.id}`, body);
      if (img.kind === "none" && item.image_url) await api.del(`/wishlist/${item.id}/image`);
    } else {
      await api.post("/wishlist", body);
    }
  }, onClose);
  const del = useWishMutation(() => api.del(`/wishlist/${item!.id}`), onClose);
  useEffect(() => {
    if (!draft) {
      save.reset();
      del.reset();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft]);

  const pickFile = async (f: File | undefined) => {
    if (!f) return;
    try {
      setImg({ kind: "data", src: await shrinkImage(f, 400) });
    } catch {
      setNote("That file isn't an image Viceroy can read.");
    }
  };

  return (
    <Dialog
      open={draft !== null}
      onOpenChange={(o) => !o && onClose()}
      title={item ? "Edit item" : "Add to wishlist"}
      footer={
        <>
          {item &&
            (confirmDelete ? (
              <Button variant="danger" className="mr-auto" loading={del.isPending} onClick={() => del.mutate()}>
                Delete item
              </Button>
            ) : (
              <Button variant="danger-ghost" className="mr-auto" aria-label="Delete item" onClick={() => setConfirmDelete(true)}>
                <Trash2 size={14} />
              </Button>
            ))}
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate()} loading={save.isPending} disabled={preview.isPending}>
            Save
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
        onPaste={(e) => {
          // Paste an image anywhere in the dialog to use it as the picture.
          const f = Array.from(e.clipboardData.files).find((x) => x.type.startsWith("image/"));
          if (f) {
            e.preventDefault();
            pickFile(f);
          }
        }}
        data-testid="wish-dialog"
      >
        <div className="relative">
          <Field
            label="Link"
            type="url"
            inputMode="url"
            placeholder="Paste a product link"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            onBlur={() => fetchLink(url)}
            onPaste={(e) => {
              const t = e.clipboardData.getData("text").trim();
              if (t) {
                e.preventDefault();
                setUrl(t);
                fetchLink(t);
              }
            }}
            autoFocus={!item}
          />
          {preview.isPending && <Loader2 size={14} className="absolute right-3 top-[34px] animate-spin text-muted" aria-label="Reading the page" />}
        </div>
        {preview.isError && <p className="-mt-1 text-xs text-negative">{preview.error.message} Fill in the details yourself.</p>}
        {note && <p className="-mt-1 text-xs text-muted" data-testid="wish-preview-note">{note}</p>}

        <div className="grid grid-cols-[6rem_1fr] gap-3">
          <div className="flex flex-col gap-1">
            <span className="text-[13px] font-medium">Picture</span>
            <div className="group relative grid size-24 place-items-center overflow-hidden rounded-lg border border-border bg-white">
              {img.kind !== "none" ? (
                <>
                  <img src={img.src} alt="" referrerPolicy="no-referrer" className="size-full object-contain" data-testid="wish-dialog-image" />
                  <button
                    type="button"
                    onClick={() => setImg({ kind: "none" })}
                    aria-label="Remove picture"
                    className="absolute right-1 top-1 grid size-5 place-items-center rounded-full bg-surface text-muted shadow hover:text-text"
                  >
                    <X size={12} />
                  </button>
                </>
              ) : (
                <button type="button" onClick={() => fileRef.current?.click()} className="flex size-full flex-col items-center justify-center gap-1 text-[11px] text-muted hover:bg-surface-2">
                  <ImagePlus size={18} />
                  Upload or paste
                </button>
              )}
            </div>
            <input ref={fileRef} type="file" accept="image/png,image/jpeg,image/webp,image/gif" hidden onChange={(e) => pickFile(e.target.files?.[0])} />
          </div>
          <div className="flex flex-col gap-3">
            <Field label="Name" value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Reading lamp" />
            <Field
              label="Price"
              inputMode="decimal"
              value={price}
              onChange={(e) => setPrice(e.target.value)}
              placeholder="Unknown"
              hint={fetchedPrice !== null && price.trim() === fetchedPrice ? "From the store's page" : undefined}
            />
          </div>
        </div>

        <div className="flex flex-col gap-1">
          <span className="text-[13px] font-medium">How much do you want it?</span>
          <StarPicker value={stars} onChange={setStars} size={20} />
        </div>

        {members.length > 1 && (
          <fieldset className="flex flex-col gap-1">
            <legend className="mb-1 text-[13px] font-medium">Wanted by</legend>
            <div className="flex flex-wrap gap-x-4 gap-y-1">
              {members.map((m) => (
                <label key={m.id} className="flex items-center gap-2 text-[13px]">
                  <input
                    type="checkbox"
                    className="accent-accent"
                    checked={wanted.includes(m.id)}
                    onChange={(e) => setWanted(e.target.checked ? [...wanted, m.id] : wanted.filter((x) => x !== m.id))}
                  />
                  {m.name}
                </label>
              ))}
            </div>
          </fieldset>
        )}

        <label className="flex items-start gap-2 text-[13px]">
          <input type="checkbox" className="mt-0.5 accent-accent" checked={saves} onChange={(e) => setSaves(e.target.checked)} />
          <span>
            Saves money long term
            <span className="block text-xs text-muted">Counts 1.5× in Best value (e.g. a water filter instead of bottled water).</span>
          </span>
        </label>

        <TextArea label="Notes" value={notes} onChange={(e) => setNotes(e.target.value)} rows={2} placeholder="Size, color, wait for a sale…" />
        {confirmDelete && <p className="text-[13px] text-negative">Delete this item from the wishlist?</p>}
        <FormError error={save.error ?? del.error} />
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}
