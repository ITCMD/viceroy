export function FormError({ error }: { error: unknown }) {
  if (!error) return null;
  const msg = error instanceof Error ? error.message : String(error);
  return <p className="rounded-lg bg-negative/10 px-3 py-2 text-[13px] text-negative">{msg}</p>;
}
