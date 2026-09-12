// Release checks finish in the background. Their toasts go to the browser that started them: an open
// accept dialog reports its own result, and settleRelease reports the rest when pick.updated arrives.
import { useEffect } from "react";
import { toast } from "sonner";
import type { Pick, ReleaseCheck, ReleaseInfo } from "@/api/types";
import { fileSize } from "./format";

/** Pick id → toast title prefix ("Added to Radarr", "Switched to …"), for checks this session started. */
const awaiting = new Map<number, string>();
/** Picks whose accept dialog is open. */
const shown = new Set<number>();

export function awaitRelease(id: number, prefix: string) {
  awaiting.set(id, prefix);
}

export const isAwaitingRelease = (id: number) => awaiting.has(id);

/** The prefix for a check this session started, forgotten once taken so the result is reported once. */
export function takeAwaitedRelease(id: number): string | undefined {
  const prefix = awaiting.get(id);
  awaiting.delete(id);
  return prefix;
}

/** While mounted, the accept dialog for this pick reports its result instead of settleRelease. */
export function useReleaseShown(id: number) {
  useEffect(() => {
    shown.add(id);
    return () => {
      shown.delete(id);
    };
  }, [id]);
}

/** For every pick.updated: toasts a finished check this session started whose dialog was closed. */
export function settleRelease(pick: Pick) {
  const check = pick.request?.release;
  if (!check || check.status === "checking" || shown.has(pick.id)) return;
  const prefix = takeAwaitedRelease(pick.id);
  if (prefix) announceRelease(prefix, check);
}

/** Toasts a finished check. `prefix` is "Added to Radarr" or "Switched to <profile>". */
export function announceRelease(prefix: string, check: ReleaseCheck) {
  const from = check.switched_from;
  const switchNote = from ? `Switched from ${from} to ${check.profile}. ` : "";
  switch (check.status) {
    case "checking":
      break;
    case "grabbed":
      toast.success(`${from ? `Switched to ${check.profile}` : prefix} · grabbing`, {
        description: `${from ? `Nothing fit ${from}. ` : ""}${check.release ? releaseLine(check.release) : ""}`,
      });
      break;
    case "pending":
      toast(`${prefix} · release on hold`, {
        description: `${switchNote}Radarr grabs ${check.release?.title ?? "the release"} after your delay profile's wait`,
      });
      break;
    case "searching":
      toast(`${prefix} · still searching`, { description: `${switchNote}Radarr grabs a release if one fits ${check.profile}` });
      break;
    case "unavailable":
      toast(`${prefix} · not released yet`, { description: `${switchNote}Radarr grabs it once it's available` });
      break;
    case "failed":
      toast.warning(`${prefix} · couldn't check the search`, { description: check.error });
      break;
    case "waiting": {
      const best = check.alternatives[0];
      const description = from
        ? `Switched from ${from} to ${check.profile}, but Radarr still found nothing to grab`
        : best
          ? `${best.name} would grab one: open the title to switch`
          : check.found === 0
            ? "No releases on your indexers yet"
            : `${check.found} found, none fit any of your profiles`;
      toast.warning(`${prefix} · waiting for a release`, { description });
      break;
    }
  }
}

/** "Remux-1080p · 32.4 GB · <title>" */
function releaseLine(r: ReleaseInfo): string {
  return [r.quality, fileSize(r.size), r.title].filter(Boolean).join(" · ");
}
