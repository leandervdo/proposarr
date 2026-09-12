// Wording and rules for the library titles an open search matched (OwnedPanel and GetOwnedDialog).
import type { OwnedTitle, ReleaseCheck } from "@/api/types";
import { releaseLabel, type StatusLabel } from "./bulkAdd";
import { fileSize } from "./format";

/** A movie in Radarr that isn't on disk or downloading, without a search Proposarr is still following. */
export function canGet(t: OwnedTitle): boolean {
  return (
    t.kind === "movies" &&
    !!t.radarr &&
    (t.status === "missing" || t.status === "unmonitored" || t.status === "unreleased") &&
    t.search?.release.status !== "checking"
  );
}

/** The search a card reports instead of the movie's state: a running one, or a finished one while the movie is still not on disk. */
export function shownSearch(t: OwnedTitle): ReleaseCheck | undefined {
  const check = t.search?.release;
  if (!check) return undefined;
  if (check.status === "checking") return check;
  return t.status === "missing" || t.status === "unmonitored" || t.status === "unreleased" ? check : undefined;
}

/** A search for an owned movie in words: releaseLabel's, with "Searched" where an add says "Added". */
export function searchLabel(check: ReleaseCheck): StatusLabel {
  const label = releaseLabel("Searched", check);
  if (check.status === "searching") return { ...label, title: "Radarr is still searching" };
  if (check.status === "failed") return { ...label, title: "Couldn't check the search" };
  return label;
}

/** In Radarr's queue, but held there for a delay profile. */
export function isOnHold(t: OwnedTitle): boolean {
  return t.status === "downloading" && t.radarr?.queue?.status === "delay";
}

/** "Downloaded · Bluray-1080p", "Downloading 45%", "Missing", or the search that is shown instead. */
export function ownedStatusLabel(t: OwnedTitle): StatusLabel {
  const check = shownSearch(t);
  if (check) return searchLabel(check);
  switch (t.status) {
    case "downloaded":
      return {
        tone: "success",
        title: t.radarr?.file_quality ? `Downloaded · ${t.radarr.file_quality}` : "Downloaded",
        description: fileSize(t.radarr?.size_on_disk) || undefined,
      };
    case "downloading": {
      const queue = t.radarr?.queue;
      if (isOnHold(t)) return { tone: "neutral", title: "On hold", description: `Radarr grabs ${queue?.title ?? "the release"} after your delay profile's wait` };
      // No progress while the size is unknown.
      const progress = queue?.progress;
      return { tone: "pending", title: progress === undefined ? "Downloading" : `Downloading ${Math.round(progress)}%`, description: queue?.title };
    }
    case "missing":
      return { tone: "warning", title: "Missing" };
    case "unmonitored":
      return { tone: "neutral", title: "Not monitored" };
    case "unreleased":
      return { tone: "neutral", title: "Not released yet" };
    case "unknown":
      return { tone: "warning", title: "Couldn't read Radarr", description: t.error };
    case "in_library":
      return { tone: "success", title: "In your library" };
  }
}
