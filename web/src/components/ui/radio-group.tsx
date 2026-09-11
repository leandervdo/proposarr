import * as RadioGroupPrimitive from "@radix-ui/react-radio-group";
import { forwardRef, type ComponentPropsWithoutRef, type ElementRef } from "react";
import { cn } from "@/lib/utils";

export const RadioGroup = forwardRef<
  ElementRef<typeof RadioGroupPrimitive.Root>,
  ComponentPropsWithoutRef<typeof RadioGroupPrimitive.Root>
>(({ className, ...props }, ref) => (
  <RadioGroupPrimitive.Root ref={ref} className={cn("grid gap-2", className)} {...props} />
));
RadioGroup.displayName = "RadioGroup";

/** A full-width option row: the whole row is the radio. */
export const RadioCard = forwardRef<
  ElementRef<typeof RadioGroupPrimitive.Item>,
  ComponentPropsWithoutRef<typeof RadioGroupPrimitive.Item>
>(({ className, children, ...props }, ref) => (
  <RadioGroupPrimitive.Item
    ref={ref}
    className={cn(
      "group flex w-full items-center gap-3 rounded-[var(--radius-control)] border border-border bg-surface-raised px-3.5 py-3 text-left text-sm transition-colors",
      "hover:border-text-muted/60 data-[state=checked]:border-accent data-[state=checked]:bg-accent-soft",
      className,
    )}
    {...props}
  >
    <span className="grid size-[18px] shrink-0 place-items-center rounded-full border-2 border-text-muted/60 transition-colors group-data-[state=checked]:border-accent">
      <RadioGroupPrimitive.Indicator className="size-2 rounded-full bg-accent" />
    </span>
    <span className="min-w-0 flex-1">{children}</span>
  </RadioGroupPrimitive.Item>
));
RadioCard.displayName = "RadioCard";
