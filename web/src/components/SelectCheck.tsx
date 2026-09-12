import { Check } from "lucide-react";
import { cn } from "@/lib/utils";

/** What a card needs to take part in a multi-select. */
export interface CardSelection {
  selected: boolean;
  /** Something is selected, so a click on the card toggles it. */
  active: boolean;
  /** `range`: shift was held, select from the last clicked card to this one. */
  onToggle: (range: boolean) => void;
}

/**
 * The round select checkbox on a card's top-left corner. Its card is a `group/select`: the checkbox shows while
 * the card is hovered or has keyboard focus, always on touch screens, and on every card once something is
 * selected (`shown`).
 */
export function SelectCheck({
  title,
  checked,
  shown,
  onToggle,
  className,
}: {
  title: string;
  checked: boolean;
  shown: boolean;
  onToggle: (range: boolean) => void;
  className?: string;
}) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      aria-label={`Select ${title}`}
      onMouseDown={(e) => {
        // Shift+click would otherwise select the text between cards.
        if (e.shiftKey) e.preventDefault();
      }}
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        onToggle(e.shiftKey);
      }}
      className={cn(
        // Sits on the poster's corner, cut out of it by a ring in the page colour.
        "absolute -top-2.5 -left-2.5 z-20 grid size-7 place-items-center rounded-full border-2 ring-[3px] ring-background",
        "shadow-[0_6px_16px_-4px_rgb(0_0_0/0.5)] transition-[opacity,background-color,border-color,color,scale,translate] duration-150 active:scale-90",
        // A larger, invisible hit area for fingers.
        "after:absolute after:-inset-2 after:rounded-full after:content-['']",
        checked
          ? "border-accent bg-accent text-accent-contrast"
          : "border-text-muted/60 bg-surface text-transparent hover:border-accent hover:text-accent/70",
        checked || shown
          ? "opacity-100"
          : [
              "pointer-events-none opacity-0",
              "group-hover/select:pointer-events-auto group-hover/select:opacity-100",
              "group-focus-within/select:pointer-events-auto group-focus-within/select:opacity-100",
              "[@media(hover:none)]:pointer-events-auto [@media(hover:none)]:opacity-100",
            ],
        className,
      )}
    >
      <Check aria-hidden className="size-4" strokeWidth={3} />
    </button>
  );
}
