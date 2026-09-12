// Release checks finish in the background. Their toasts go to the browser that started them: an open dialog
// reports its own result, and settleRelease and settleOwnedRelease report the rest when pick.updated or
// owned.updated arrives.
import { useEffect } from "react";
import { toast } from "sonner";
import type { OwnedTitle, Pick, ReleaseCheck, ReleaseInfo } from "@/api/types";
import { fileSize } from "./format";

/** A pick id, or ownedReleaseKey(tmdbId) for a search for a movie that is already in Radarr. */
export type ReleaseKey = number | `movie:${number}`;

export const ownedReleaseKey = (tmdbId: number): ReleaseKey => `movie:${tmdbId}`;

/** Key → toast title prefix ("Added to Radarr", "Switched to …", a movie's title), for checks this session started. */
const awaiting = new Map<ReleaseKey, string>();
/** Checks a dialog is showing. */
const shown = new Set<ReleaseKey>();

export function awaitRelease(key: ReleaseKey, prefix: string) {
  awaiting.set(key, prefix);
}

export const isAwaitingRelease = (key: ReleaseKey) => awaiting.has(key);

/** The prefix for a check this session started, forgotten once taken so the result is reported once. */
export function takeAwaitedRelease(key: ReleaseKey): string | undefined {
  const prefix = awaiting.get(key);
  awaiting.delete(key);
  return prefix;
}

/** While mounted, the dialog showing this check reports its result instead of a toast. */
export function useReleaseShown(key: ReleaseKey) {
  useEffect(() => {
    shown.add(key);
    return () => {
      shown.delete(key);
    };
  }, [key]);
}

/** For every pick.updated: toasts a finished check this session started whose dialog was closed. */
export function settleRelease(pick: Pick) {
  settle(pick.id, pick.request?.release);
}

/** For every owned.updated: toasts a finished search this session started whose dialog was closed. */
export function settleOwnedRelease(title: OwnedTitle) {
  settle(ownedReleaseKey(title.tmdb_id), title.search?.release, "search again with that profile");
}

function settle(key: ReleaseKey, check: ReleaseCheck | undefined, switchHint?: string) {
  if (!check || check.status === "checking" || shown.has(key)) return;
  const prefix = takeAwaitedRelease(key);
  if (prefix) announceRelease(prefix, check, switchHint);
}

/**
 * Toasts a finished check. `prefix` is "Added to Radarr", "Switched to <profile>" or the title of a movie searched
 * for again; `switchHint` says how to use another profile that would grab a release.
 */
export function announceRelease(prefix: string, check: ReleaseCheck, switchHint = "open the title to switch") {
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
          ? `${best.name} would grab one: ${switchHint}`
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
