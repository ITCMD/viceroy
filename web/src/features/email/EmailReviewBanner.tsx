import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight, Mail } from "lucide-react";
import { mailboxesQuery, openMessagesQuery } from "./api";

/** "N alert emails need a filter" on the Transactions page, linking to Settings. */
export function EmailReviewBanner() {
  const { data: boxes } = useQuery(mailboxesQuery);
  const { data } = useQuery({ ...openMessagesQuery, enabled: (boxes?.length ?? 0) > 0 });
  const failed = data?.counts.parse_failed ?? 0;
  const unrouted = data?.counts.unrouted ?? 0;
  if (!boxes?.length || failed + unrouted === 0) return null;
  const text =
    failed > 0
      ? `${failed} alert ${failed === 1 ? "email" : "emails"} couldn't be read`
      : `${unrouted} ${unrouted === 1 ? "email needs" : "emails need"} a filter`;
  return (
    <Link
      to={"/settings" as string}
      search={{ tab: "email" } as never}
      hash="emails-to-review"
      className="flex items-center gap-3 rounded-xl border border-accent/40 bg-accent-soft px-4 py-3 text-left text-sm text-accent"
      data-testid="email-review-banner"
    >
      <Mail size={16} />
      <span className="flex-1 font-medium">{text}</span>
      <ChevronRight size={16} />
    </Link>
  );
}
