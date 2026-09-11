import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export function PageHeader({ title, children, actions, className }: { title: string; children?: ReactNode; actions?: ReactNode; className?: string }) {
  return (
    <header className={cn("mb-8 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between", className)}>
      <div className="min-w-0">
        <h1 className="font-display text-[44px] leading-[0.9] font-bold tracking-tight sm:text-[56px]">{title}</h1>
        {children && <div className="mt-3 max-w-[62ch] text-[15px] text-text-muted">{children}</div>}
      </div>
      {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
    </header>
  );
}

export function SectionTitle({ children, aside }: { children: ReactNode; aside?: ReactNode }) {
  return (
    <div className="mb-4 flex items-baseline justify-between gap-4">
      <h2 className="font-display text-[26px] leading-none font-bold tracking-tight">{children}</h2>
      {aside && <div className="text-sm text-text-muted">{aside}</div>}
    </div>
  );
}
