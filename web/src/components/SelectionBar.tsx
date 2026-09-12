import { Clock, EyeOff, Plus, X } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useRef, useState } from "react";
import type { Kind, Pick } from "@/api/types";
import { appFor, appName, plural } from "@/lib/format";
import type { Selection } from "@/lib/useSelection";
import { BulkAddDialog, type BulkAddRequest } from "./BulkAddDialog";
import { Button } from "./ui/button";
import { Tooltip } from "./ui/tooltip";
import { useBulkVerdict } from "./useBulkVerdict";

/**
 * The bar for a multi-select of picks: count, select all, bulk add, Later, Ignore and clear. It floats at the
 * bottom of the viewport while anything is selected, above the bottom navigation on phones.
 */
export function SelectionBar({ selection, kind }: { selection: Selection<Pick>; kind: Kind }) {
  const name = appName(appFor(kind));
  const bulkVerdict = useBulkVerdict();
  const [adding, setAdding] = useState<BulkAddRequest | null>(null);
  const submitted = useRef(false);

  const { selected, count } = selection;
  // Added titles are not added again, and keep their verdict.
  const open = selected.filter((p) => p.request?.status !== "added");
  const alreadyAdded = count - open.length;

  const decide = (verdict: "later" | "ignored") => {
    const note = alreadyAdded > 0 ? `${plural(alreadyAdded, "title")} already in ${name} left as ${alreadyAdded === 1 ? "it was" : "they were"}` : undefined;
    selection.clear();
    void bulkVerdict(open, verdict, note);
  };

  const startAdding = () => {
    submitted.current = false;
    setAdding({ kind, picks: open, skipped: alreadyAdded });
  };

  const addButton = (
    <Button variant="primary" onClick={startAdding} disabled={open.length === 0} className="w-full sm:w-auto" aria-label={`Add ${plural(open.length, "title")} to ${name}`}>
      <Plus /> Add to {name}
      {open.length > 0 && <span className="nums -mr-1 rounded-full bg-accent-contrast/15 px-1.5 text-xs leading-5 font-semibold">{open.length}</span>}
    </Button>
  );

  return (
    <>
      {/* Room below the last row, so the bar never covers it. */}
      {count > 0 && <div aria-hidden className="h-28 sm:h-20" />}

      <AnimatePresence>
        {count > 0 && (
          <motion.div
            key="selection-bar"
            initial={{ y: 40, opacity: 0 }}
            animate={{ y: 0, opacity: 1 }}
            exit={{ y: 40, opacity: 0, transition: { duration: 0.16, ease: "easeIn" } }}
            transition={{ type: "spring", stiffness: 560, damping: 40, mass: 0.8 }}
            className="pointer-events-none fixed inset-x-0 bottom-[calc(4rem+env(safe-area-inset-bottom)+0.625rem)] z-40 flex justify-center px-3 sm:px-6 lg:bottom-6 lg:left-60 lg:px-10"
          >
            <div
              role="toolbar"
              aria-label="Selected titles"
              className="pointer-events-auto flex w-full max-w-[54rem] flex-col gap-1.5 rounded-[var(--radius-panel)] border border-border bg-surface/95 p-2 shadow-[0_28px_70px_-18px_rgb(0_0_0/0.6)] backdrop-blur-xl sm:flex-row sm:items-center sm:gap-2 dark:bg-surface-raised/95"
            >
              <div className="flex min-w-0 items-center gap-1">
                <p aria-live="polite" aria-atomic="true" className="flex items-baseline gap-1.5 pr-1 pl-2.5 whitespace-nowrap">
                  <span className="nums font-display text-[30px] leading-none font-bold text-accent">{count}</span>
                  <span className="text-sm font-medium">selected</span>
                </p>
                <Button variant="ghost" size="sm" onClick={selection.selectAll} disabled={selection.allSelected}>
                  Select all
                  <span className="nums text-xs text-text-muted">{selection.total}</span>
                </Button>
                <Button variant="ghost" size="icon" onClick={selection.clear} aria-label="Clear selection" className="ml-auto sm:hidden">
                  <X />
                </Button>
              </div>

              <div className="flex items-center gap-2 sm:ml-auto">
                <Button onClick={() => decide("later")} disabled={open.length === 0} className="max-[420px]:px-3">
                  <Clock />
                  <span className="max-[420px]:sr-only">Later</span>
                </Button>
                <Button onClick={() => decide("ignored")} disabled={open.length === 0} className="max-[420px]:px-3">
                  <EyeOff />
                  <span className="max-[420px]:sr-only">Ignore</span>
                </Button>
                {open.length === 0 ? (
                  <Tooltip content={`Every selected title is already in ${name}`}>
                    {/* A disabled button gets no pointer events; the wrapper carries the tooltip. */}
                    <span tabIndex={0} className="min-w-0 flex-1 rounded-[var(--radius-control)] sm:flex-none">
                      {addButton}
                    </span>
                  </Tooltip>
                ) : (
                  <div className="min-w-0 flex-1 sm:flex-none">{addButton}</div>
                )}
                <Button variant="ghost" size="icon" onClick={selection.clear} aria-label="Clear selection" className="hidden sm:inline-flex">
                  <X />
                </Button>
              </div>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      <BulkAddDialog
        request={adding}
        onSubmitted={() => {
          submitted.current = true;
        }}
        onClose={() => {
          setAdding(null);
          if (submitted.current) selection.clear();
        }}
      />
    </>
  );
}
