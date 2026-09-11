import * as ToggleGroup from "@radix-ui/react-toggle-group";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

interface Option<T extends string> {
  value: T;
  label: ReactNode;
  count?: number;
}

interface SegmentedProps<T extends string> {
  value: T;
  onChange: (value: T) => void;
  options: Option<T>[];
  label: string;
  size?: "sm" | "md";
  className?: string;
}

/** Single-choice toggle row that can never be emptied. */
export function Segmented<T extends string>({ value, onChange, options, label, size = "md", className }: SegmentedProps<T>) {
  return (
    <ToggleGroup.Root
      type="single"
      value={value}
      onValueChange={(v) => v && onChange(v as T)}
      aria-label={label}
      className={cn("inline-flex rounded-[var(--radius-control)] border border-border bg-surface p-0.5", className)}
    >
      {options.map((o) => (
        <ToggleGroup.Item
          key={o.value}
          value={o.value}
          className={cn(
            "inline-flex items-center gap-1.5 rounded-[6px] font-medium whitespace-nowrap text-text-muted transition-colors hover:text-text",
            "data-[state=on]:bg-surface-raised data-[state=on]:text-text data-[state=on]:shadow-[inset_0_0_0_1px_var(--border)]",
            size === "sm" ? "h-7 px-2.5 text-[13px]" : "h-9 px-3.5 text-sm",
          )}
        >
          {o.label}
          {o.count !== undefined && <span className="nums text-xs text-text-muted">{o.count}</span>}
        </ToggleGroup.Item>
      ))}
    </ToggleGroup.Root>
  );
}
