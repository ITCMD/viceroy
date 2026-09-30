import clsx from "clsx";
import { formatMoney } from "@/lib/format";

/** Renders integer cents. `colored` makes income green; expenses stay neutral like Monarch. */
export function MoneyText({
  cents,
  colored,
  whole,
  className,
}: {
  cents: number;
  colored?: boolean;
  whole?: boolean;
  className?: string;
}) {
  return (
    <span className={clsx("tabular", colored && cents > 0 && "text-positive", className)}>
      {formatMoney(cents, { whole })}
    </span>
  );
}
