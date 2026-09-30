/** HTML legend row for multi-series charts: a color swatch per series, text in text tokens. */
export function Legend({ items }: { items: { name: string; color: string; dashed?: boolean }[] }) {
  return (
    <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
      {items.map((it) => (
        <li key={it.name} className="flex items-center gap-1.5">
          <span
            aria-hidden
            className="inline-block"
            style={
              it.dashed
                ? { width: 12, height: 0, borderTop: `2px dashed ${it.color}` }
                : { width: 8, height: 8, borderRadius: 2, background: it.color }
            }
          />
          {it.name}
        </li>
      ))}
    </ul>
  );
}
