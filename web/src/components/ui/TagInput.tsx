import { X } from "lucide-react";
import { useId, useState } from "react";

/** Chip list with a text box; Enter or comma adds a tag, suggestions come from existing tags. */
export function TagInput({
  label,
  value,
  onChange,
  suggestions,
}: {
  label: string;
  value: string[];
  onChange: (tags: string[]) => void;
  suggestions: string[];
}) {
  const id = useId();
  const [text, setText] = useState("");
  const add = (raw: string) => {
    const t = raw.trim();
    if (t && !value.some((v) => v.toLowerCase() === t.toLowerCase())) onChange([...value, t]);
    setText("");
  };
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-[13px] font-medium text-text">
        {label}
      </label>
      <div className="flex min-h-9 flex-wrap items-center gap-1.5 rounded-lg border border-border bg-surface px-2 py-1 transition focus-within:border-accent focus-within:ring-2 focus-within:ring-accent/20">
        {value.map((t) => (
          <span key={t} className="inline-flex items-center gap-1 rounded-full bg-surface-2 py-0.5 pl-2 pr-1 text-xs">
            {t}
            <button type="button" aria-label={`Remove ${t}`} onClick={() => onChange(value.filter((v) => v !== t))} className="rounded-full p-0.5 text-muted hover:text-text">
              <X size={11} />
            </button>
          </span>
        ))}
        <input
          id={id}
          list={`${id}-list`}
          value={text}
          onChange={(e) => (e.target.value.endsWith(",") ? add(e.target.value.slice(0, -1)) : setText(e.target.value))}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              add(text);
            } else if (e.key === "Backspace" && !text && value.length) {
              onChange(value.slice(0, -1));
            }
          }}
          onBlur={() => text && add(text)}
          placeholder={value.length ? "" : "Add a tag"}
          className="h-7 min-w-24 flex-1 bg-transparent text-sm outline-none placeholder:text-muted"
        />
        <datalist id={`${id}-list`}>
          {suggestions.filter((s) => !value.includes(s)).map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>
      </div>
    </div>
  );
}
