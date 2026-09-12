import type { QualityProfile } from "@/api/types";

export interface FallbackTargets {
  /** Whether "switch if nothing fits" can be offered for the chosen profile. */
  available: boolean;
  /** unranked: no ranking is set, or the chosen profile isn't in it. last: nothing is ranked below it. */
  reason?: "unranked" | "last";
  /** The profiles Proposarr would try, top to bottom. */
  lower: QualityProfile[];
}

/**
 * The profiles ranked below the chosen one. The chosen profile is a ceiling: a fallback only goes down the
 * user's ranking (`radarr.profile_order`, ids or names best first), never up. Entries Radarr no longer has are skipped.
 */
export function fallbackTargets(profileOrder: string | undefined, chosenProfileId: number, profiles: QualityProfile[]): FallbackTargets {
  const ranking: QualityProfile[] = [];
  for (const entry of (profileOrder ?? "").split(",").map((e) => e.trim().toLowerCase())) {
    // The UI saves ids; the config file and environment may use names, as the server allows.
    const profile = profiles.find((p) => String(p.id) === entry || p.name.toLowerCase() === entry);
    if (profile && !ranking.includes(profile)) ranking.push(profile);
  }
  const rank = ranking.findIndex((p) => p.id === chosenProfileId);
  if (rank < 0) return { available: false, reason: "unranked", lower: [] };
  const lower = ranking.slice(rank + 1);
  return lower.length > 0 ? { available: true, lower } : { available: false, reason: "last", lower };
}
