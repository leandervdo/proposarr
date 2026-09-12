// Wording and rules for adding several titles at once (BulkAddDialog).
//
// The "if nothing fits" rules come from lib/fallback.ts, shared with AcceptDialog. The release labels mirror
// lib/releases.ts (announceRelease) and AcceptDialog's wording.
import type { IfNothingFits, ReleaseCheck, ReleaseInfo } from "@/api/types";
import { type FallbackTargets } from "./fallback";
import { fileSize } from "./format";

export { fallbackTargets, type FallbackTargets } from "./fallback";

export const IF_NOTHING_FITS_LEGEND = "If nothing fits this profile";

export const IF_NOTHING_FITS_OPTIONS: { value: IfNothingFits; label: string }[] = [
  { value: "switch", label: "Switch to the next lower-ranked profile that finds it" },
  { value: "wait", label: "Keep waiting" },
];

/** The value sent for a row: the user's explicit choice, else "switch" when it is available; otherwise "wait". */
export function effectiveFallback(choice: IfNothingFits | undefined, targets: FallbackTargets): IfNothingFits {
  if (!targets.available) return "wait";
  return choice ?? "switch";
}

/** "Tries A, then B." / "Nothing is ranked below this profile." / the Connections hint. */
export function fallbackHint(targets: FallbackTargets): string {
  if (targets.available) {
    const names = targets.lower.map((p) => p.name);
    if (names.length === 0) return "Tries the next lower-ranked profile.";
    return `Tries ${names.length === 1 ? names[0] : `${names.slice(0, -1).join(", ")}, then ${names.at(-1)}`}.`;
  }
  return targets.reason === "last" ? "Nothing is ranked below this profile." : UNRANKED_HINT;
}

/** The hint for an unranked profile; the UI links "Connections". */
export const UNRANKED_HINT = "Rank your quality profiles under Connections to fall back automatically.";

export type StatusTone = "pending" | "success" | "neutral" | "warning" | "danger";

export interface StatusLabel {
  tone: StatusTone;
  title: string;
  description?: string;
}

/**
 * A release check in words. `prefix` is "Added" or "Switched to <profile>". Same wording as announceRelease's
 * toasts and AcceptDialog's searching and waiting screens.
 */
export function releaseLabel(prefix: string, check: ReleaseCheck): StatusLabel {
  const from = check.switched_from;
  const switchNote = from ? `Switched from ${from} to ${check.profile}. ` : "";
  switch (check.status) {
    case "checking":
      return { tone: "pending", title: `Radarr is searching with ${check.profile}…` };
    case "grabbed":
      return {
        tone: "success",
        title: `${from ? `Switched to ${check.profile}` : prefix} · grabbing`,
        description: `${from ? `Nothing fit ${from}. ` : ""}${check.release ? releaseLine(check.release) : ""}` || undefined,
      };
    case "pending":
      return {
        tone: "neutral",
        title: `${prefix} · release on hold`,
        description: `${switchNote}Radarr grabs ${check.release?.title ?? "the release"} after your delay profile's wait`,
      };
    case "searching":
      return { tone: "neutral", title: `${prefix} · still searching`, description: `${switchNote}Radarr grabs a release if one fits ${check.profile}` };
    case "unavailable":
      return { tone: "neutral", title: `${prefix} · not released yet`, description: `${switchNote}Radarr grabs it once it's available` };
    case "failed":
      return { tone: "warning", title: `${prefix} · couldn't check the search`, description: check.error };
    case "waiting":
      return {
        tone: "warning",
        title: `${prefix} · waiting for a release`,
        description: from
          ? `Switched from ${from} to ${check.profile}, but Radarr still found nothing to grab`
          : check.found === 0
            ? "No releases on your indexers yet"
            : `${check.found} found, none fit ${check.profile}`,
      };
  }
}

/** "Remux-1080p · 32.4 GB · <title>" */
export function releaseLine(r: ReleaseInfo): string {
  return [r.quality, fileSize(r.size), r.title].filter(Boolean).join(" · ");
}

/** Where one row of the bulk add stands. Added movies report their release check's status. */
export type RowOutcome = "queued" | "adding" | "added" | "exists" | "error" | ReleaseCheck["status"];

const SUMMARY: [RowOutcome, string][] = [
  ["adding", "adding"],
  ["queued", "queued"],
  ["checking", "checking"],
  ["grabbed", "grabbed"],
  ["pending", "on hold"],
  ["searching", "still searching"],
  ["unavailable", "not released yet"],
  ["waiting", "waiting"],
  ["failed", "not checked"],
  ["exists", "already in library"],
  ["error", "failed"],
];

/** "5 added · 3 grabbed · 1 waiting · 1 failed". `words` renames parts, e.g. "started" for searches instead of "added". */
export function bulkSummary(outcomes: readonly RowOutcome[], words: Partial<Record<RowOutcome, string>> = {}): string {
  const added = outcomes.filter((o) => o !== "queued" && o !== "adding" && o !== "exists" && o !== "error").length;
  const parts = added > 0 ? [`${added} ${words.added ?? "added"}`] : [];
  for (const [outcome, label] of SUMMARY) {
    const n = outcomes.filter((o) => o === outcome).length;
    if (n > 0) parts.push(`${n} ${words[outcome] ?? label}`);
  }
  return parts.join(" · ");
}
