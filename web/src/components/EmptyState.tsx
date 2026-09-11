import { PlugZap, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";

interface EmptyStateProps {
  icon: LucideIcon;
  title: string;
  children?: ReactNode;
  actions?: ReactNode;
  tone?: "neutral" | "danger" | "warning";
  className?: string;
}

export function EmptyState({ icon: Icon, title, children, actions, tone = "neutral", className }: EmptyStateProps) {
  return (
    <section className={cn("mx-auto flex max-w-lg flex-col items-center px-4 py-16 text-center sm:py-24", className)}>
      <div
        className={cn(
          "grid size-16 place-items-center rounded-full border",
          tone === "danger" && "border-danger/40 bg-danger/10 text-danger",
          tone === "warning" && "border-warning/40 bg-warning/10 text-warning",
          tone === "neutral" && "border-border bg-surface text-accent",
        )}
      >
        <Icon className="size-7" strokeWidth={1.75} />
      </div>
      <h2 className="mt-6 font-display text-[34px] leading-none font-bold tracking-tight">{title}</h2>
      {children && <div className="mt-3 text-[15px] leading-relaxed text-text-muted [&_code]:rounded [&_code]:bg-surface-raised [&_code]:px-1.5 [&_code]:py-0.5 [&_code]:text-[13px] [&_code]:text-text">{children}</div>}
      {actions && <div className="mt-7 flex flex-wrap justify-center gap-2">{actions}</div>}
    </section>
  );
}

export function BackendDown({ onRetry }: { onRetry: () => void }) {
  return (
    <EmptyState
      icon={PlugZap}
      tone="danger"
      title="Can't reach Proposarr"
      actions={
        <Button variant="primary" onClick={onRetry}>
          Try again
        </Button>
      }
    >
      <p>
        The page loaded, but the Proposarr server is not answering. Check that <code>proposarr serve</code> is running and that this
        address points to it.
      </p>
    </EmptyState>
  );
}

export function ErrorNote({ title, message, onRetry }: { title: string; message: string; onRetry?: () => void }) {
  return (
    <div role="alert" className="flex flex-col gap-3 rounded-[var(--radius-panel)] border border-danger/35 bg-danger/8 p-4 sm:flex-row sm:items-center">
      <div className="min-w-0 flex-1">
        <p className="font-semibold text-danger">{title}</p>
        <p className="mt-0.5 text-sm break-words text-text-muted">{message}</p>
      </div>
      {onRetry && (
        <Button size="sm" onClick={onRetry}>
          Try again
        </Button>
      )}
    </div>
  );
}
