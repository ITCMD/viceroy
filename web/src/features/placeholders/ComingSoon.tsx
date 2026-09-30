import { Card, EmptyState, PageHeader } from "@/components/ui";
import type { NavItem } from "@/components/nav";

/** Temporary page body for tabs not yet built (phase noted in docs/PLAN.md). */
export function ComingSoon({ item, phase }: { item: NavItem; phase: number }) {
  return (
    <>
      <PageHeader title={item.label} />
      <div className="p-4 md:p-6">
        <Card>
          <EmptyState icon={item.icon} title={`${item.label} is coming`}>
            Planned for phase {phase} of the build.
          </EmptyState>
        </Card>
      </div>
    </>
  );
}
