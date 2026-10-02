import { Star } from "lucide-react";

/** 1-5 stars, clickable in place. */
export function StarPicker({ value, onChange, size = 16, label = "Stars" }: { value: number; onChange: (v: number) => void; size?: number; label?: string }) {
  return (
    <div className="flex" role="radiogroup" aria-label={label}>
      {[1, 2, 3, 4, 5].map((n) => (
        <button
          key={n}
          type="button"
          role="radio"
          aria-checked={value === n}
          aria-label={`${n} star${n > 1 ? "s" : ""}`}
          onClick={(e) => {
            e.stopPropagation();
            onChange(n);
          }}
          className="p-0.5 text-accent hover:scale-110"
        >
          <Star size={size} fill={n <= value ? "currentColor" : "none"} className={n <= value ? "" : "text-muted"} />
        </button>
      ))}
    </div>
  );
}
