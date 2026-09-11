import * as SelectPrimitive from "@radix-ui/react-select";
import { Check, ChevronDown } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

interface SelectProps {
  value: string;
  onValueChange: (value: string) => void;
  label: string;
  placeholder?: string;
  children: ReactNode;
  className?: string;
}

export function Select({ value, onValueChange, label, placeholder, children, className }: SelectProps) {
  return (
    <SelectPrimitive.Root value={value} onValueChange={onValueChange}>
      <SelectPrimitive.Trigger
        aria-label={label}
        className={cn(
          "inline-flex h-9 min-w-0 items-center justify-between gap-2 rounded-[var(--radius-control)] border border-border bg-surface px-3 text-sm text-text transition-colors hover:border-text-muted/60 data-[placeholder]:text-text-muted",
          className,
        )}
      >
        <span className="truncate">
          <SelectPrimitive.Value placeholder={placeholder} />
        </span>
        <SelectPrimitive.Icon>
          <ChevronDown className="size-4 text-text-muted" />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>
      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          position="popper"
          sideOffset={6}
          className="z-50 max-h-[min(24rem,var(--radix-select-content-available-height))] min-w-[var(--radix-select-trigger-width)] overflow-hidden rounded-[var(--radius-control)] border border-border bg-surface-raised p-1 text-sm text-text shadow-[0_16px_48px_-8px_rgb(0_0_0/0.45)] data-[state=open]:animate-pop-in"
        >
          <SelectPrimitive.Viewport>{children}</SelectPrimitive.Viewport>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  );
}

export function SelectItem({ value, children }: { value: string; children: ReactNode }) {
  return (
    <SelectPrimitive.Item
      value={value}
      className="relative flex cursor-pointer items-center rounded-[6px] py-2 pr-8 pl-2.5 outline-none select-none data-[highlighted]:bg-surface data-[state=checked]:text-accent"
    >
      <SelectPrimitive.ItemText>{children}</SelectPrimitive.ItemText>
      <SelectPrimitive.ItemIndicator className="absolute right-2.5">
        <Check className="size-4" />
      </SelectPrimitive.ItemIndicator>
    </SelectPrimitive.Item>
  );
}
