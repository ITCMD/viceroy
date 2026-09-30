import type { ReactNode } from "react";
import { AppLogo, cachedLogo } from "@/components/AppLogo";

export function AuthLayout({ title, subtitle, children }: { title: string; subtitle?: string; children: ReactNode }) {
  return (
    <div className="flex min-h-full items-center justify-center p-4">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex flex-col items-center gap-3 text-center">
          <AppLogo logo={cachedLogo()} size={44} />
          <div>
            <h1 className="text-xl font-semibold">{title}</h1>
            {subtitle && <p className="mt-1 text-sm text-muted">{subtitle}</p>}
          </div>
        </div>
        <div className="rounded-xl border border-border bg-surface p-6">{children}</div>
      </div>
    </div>
  );
}
