import { useEffect } from "react";
import { useBlocker } from "react-router";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle } from "../ui/dialog";

/** Asks before navigating away, or closing the tab, while there are unsaved edits. */
export function UnsavedGuard({ when }: { when: boolean }) {
  const blocker = useBlocker(({ currentLocation, nextLocation }) => when && currentLocation.pathname !== nextLocation.pathname);

  useEffect(() => {
    if (!when) return;
    const onBeforeUnload = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [when]);

  return (
    <Dialog open={blocker.state === "blocked"} onOpenChange={(open) => !open && blocker.reset?.()}>
      <DialogContent className="sm:w-[min(92vw,28rem)]">
        <div className="px-5 pt-6 pb-5 pr-14">
          <DialogTitle>Leave without saving?</DialogTitle>
          <DialogDescription className="mt-3 text-[15px]">Changes you haven't saved on this page will be lost.</DialogDescription>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => blocker.reset?.()}>
            Stay here
          </Button>
          <Button variant="primary" onClick={() => blocker.proceed?.()}>
            Leave page
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
