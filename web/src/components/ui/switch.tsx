import { forwardRef, type ButtonHTMLAttributes } from "react";
import { cn } from "@/lib/utils";

interface SwitchProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "onChange" | "role" | "type"> {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}

/** An on/off switch whose children are its visible label. */
export const Switch = forwardRef<HTMLButtonElement, SwitchProps>(({ checked, onCheckedChange, className, children, ...props }, ref) => (
  <button
    ref={ref}
    type="button"
    role="switch"
    aria-checked={checked}
    data-state={checked ? "on" : "off"}
    onClick={() => onCheckedChange(!checked)}
    className={cn(
      "group inline-flex h-9 items-center gap-2.5 rounded-[var(--radius-control)] px-2 text-sm font-medium whitespace-nowrap text-text-muted transition-colors",
      "hover:text-text data-[state=on]:text-text disabled:opacity-45",
      className,
    )}
    {...props}
  >
    {children}
    <span
      aria-hidden
      className="relative h-5 w-9 shrink-0 rounded-full border border-border bg-surface-raised transition-colors group-hover:border-text-muted/60 group-data-[state=on]:border-accent group-data-[state=on]:bg-accent"
    >
      <span className="absolute top-1/2 left-0.5 size-3.5 -translate-y-1/2 rounded-full bg-text-muted transition-[translate,background-color] duration-200 group-data-[state=on]:translate-x-4 group-data-[state=on]:bg-accent-contrast" />
    </span>
  </button>
));
Switch.displayName = "Switch";
