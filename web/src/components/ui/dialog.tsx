import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { forwardRef, type ComponentPropsWithoutRef, type ElementRef, type HTMLAttributes } from "react";
import { cn } from "@/lib/utils";

export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;

export const DialogContent = forwardRef<
  ElementRef<typeof DialogPrimitive.Content>,
  ComponentPropsWithoutRef<typeof DialogPrimitive.Content>
>(({ className, children, ...props }, ref) => (
  <DialogPrimitive.Portal>
    <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/60 backdrop-blur-sm data-[state=open]:animate-overlay-in" />
    <DialogPrimitive.Content
      ref={ref}
      className={cn(
        "fixed z-50 flex max-h-[92dvh] flex-col overflow-hidden border border-border bg-surface text-text shadow-[0_24px_80px_-12px_rgb(0_0_0/0.55)] focus:outline-none",
        // Bottom sheet on phones, centred panel from sm up.
        "inset-x-0 bottom-0 rounded-t-[var(--radius-panel)] data-[state=open]:animate-sheet-in",
        "sm:inset-auto sm:top-1/2 sm:left-1/2 sm:w-[min(92vw,34rem)] sm:-translate-x-1/2 sm:-translate-y-1/2 sm:rounded-[var(--radius-panel)] sm:data-[state=open]:animate-dialog-in",
        className,
      )}
      {...props}
    >
      {children}
      <DialogPrimitive.Close
        className="absolute top-3 right-3 z-10 grid size-9 place-items-center rounded-full text-text-muted transition-colors hover:bg-surface-raised hover:text-text"
        aria-label="Close"
      >
        <X className="size-4" />
      </DialogPrimitive.Close>
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>
));
DialogContent.displayName = "DialogContent";

/** A wide panel from sm up and a full-screen sheet on phones. The close button floats over artwork. */
export const DialogDetailContent = forwardRef<
  ElementRef<typeof DialogPrimitive.Content>,
  ComponentPropsWithoutRef<typeof DialogPrimitive.Content>
>(({ className, children, ...props }, ref) => (
  <DialogPrimitive.Portal>
    <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm data-[state=open]:animate-overlay-in" />
    <DialogPrimitive.Content
      ref={ref}
      className={cn(
        "fixed inset-0 z-50 flex flex-col overflow-hidden bg-surface text-text focus:outline-none data-[state=open]:animate-detail-in",
        "sm:inset-auto sm:top-1/2 sm:left-1/2 sm:max-h-[min(92dvh,64rem)] sm:w-[min(94vw,66rem)] sm:-translate-x-1/2 sm:-translate-y-1/2",
        "sm:rounded-[var(--radius-panel)] sm:border sm:border-border sm:shadow-[0_40px_120px_-20px_rgb(0_0_0/0.75)]",
        className,
      )}
      {...props}
    >
      {children}
      <DialogPrimitive.Close
        className="absolute top-[max(0.75rem,env(safe-area-inset-top))] right-3 z-20 grid size-10 place-items-center rounded-full bg-black/50 text-white backdrop-blur-md transition-colors hover:bg-black/70 sm:top-4 sm:right-4"
        aria-label="Close"
      >
        <X className="size-5" />
      </DialogPrimitive.Close>
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>
));
DialogDetailContent.displayName = "DialogDetailContent";

export const DialogTitle = forwardRef<
  ElementRef<typeof DialogPrimitive.Title>,
  ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title ref={ref} className={cn("font-display text-3xl leading-none font-semibold tracking-tight", className)} {...props} />
));
DialogTitle.displayName = "DialogTitle";

export const DialogDescription = forwardRef<
  ElementRef<typeof DialogPrimitive.Description>,
  ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description ref={ref} className={cn("text-sm text-text-muted", className)} {...props} />
));
DialogDescription.displayName = "DialogDescription";

export function DialogFooter({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "flex flex-col-reverse gap-2 border-t border-border bg-surface-raised px-5 py-4 pb-[max(1rem,env(safe-area-inset-bottom))] sm:flex-row sm:justify-end sm:pb-4",
        className,
      )}
      {...props}
    />
  );
}
