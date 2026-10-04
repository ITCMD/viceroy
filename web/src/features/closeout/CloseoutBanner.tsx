import { useQuery } from "@tanstack/react-query";
import { CalendarCheck, ChevronRight } from "lucide-react";
import { useState } from "react";
import { monthLabel } from "@/features/budget/api";
import { closeoutStatusQuery } from "./api";
import { CloseoutDialog } from "./CloseoutDialog";

const shortDate = (d: string) =>
  new Date(d + "T00:00:00").toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  });

/** "Close out September" from a month's last 2 days through the next month's first week.
 * With showClosed, a quiet link to the finished close-out stays until the window ends. */
export function CloseoutBanner({ showClosed = false }: { showClosed?: boolean }) {
  const { data: st } = useQuery(closeoutStatusQuery);
  const [open, setOpen] = useState(false);
  // The dialog stays mounted after closing so it can go on to next month's outlook.
  if (!st?.month) return null;
  const name = monthLabel(st.month).split(" ")[0];
  return (
    <>
      {st.closed ? (
        showClosed && (
          <button
            type="button"
            onClick={() => setOpen(true)}
            className="flex items-center gap-2 self-start text-[13px] text-muted hover:text-text"
            data-testid="closeout-done"
          >
            <CalendarCheck size={14} className="text-positive" />
            {name} is closed out · <span className="font-medium text-accent">View</span>
          </button>
        )
      ) : (
        <button
          type="button"
          onClick={() => setOpen(true)}
          className="flex w-full items-center gap-3 rounded-xl border border-accent/40 bg-accent-soft px-4 py-3 text-left text-sm text-accent"
          data-testid="closeout-banner"
        >
          <CalendarCheck size={16} />
          <span className="flex-1">
            <span className="font-medium">Close out {name}</span>
            <span className="hidden text-accent/80 sm:inline">
              {" "}
              · review what went over and under and put leftover money to work. Open until {shortDate(st.closes)}.
            </span>
          </span>
          <ChevronRight size={16} />
        </button>
      )}
      <CloseoutDialog month={st.month} open={open} onOpenChange={setOpen} />
    </>
  );
}
