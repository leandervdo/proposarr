import { Check, Clock, EyeOff, Plus, RotateCcw, Tv } from "lucide-react";
import { useEffect, useRef, useState, type KeyboardEvent, type MouseEvent } from "react";
import { Link } from "react-router";
import type { Pick } from "@/api/types";
import { appFor, appName, shortDate } from "@/lib/format";
import { ratingScore } from "@/lib/ratings";
import { useOpenTitle, useTitleLink } from "@/lib/titleModal";
import { cn } from "@/lib/utils";
import { RatingChips, TitleLinks } from "./PickMeta";
import { Poster } from "./Poster";
import { Button } from "./ui/button";
import { useVerdictActions } from "./useVerdictActions";

interface PickCardProps {
  pick: Pick;
  onAccept: (pick: Pick) => void;
  /** The pick comes from an open search: every pick is free and the score comes from ratings. */
  openSearch?: boolean;
}

export function PickCard({ pick, onAccept, openSearch = false }: PickCardProps) {
  const [revealed, setRevealed] = useState(false);
  const { decide } = useVerdictActions();
  const openTitle = useOpenTitle();
  const titleLink = useTitleLink();
  const cardRef = useRef<HTMLElement>(null);
  const pointerType = useRef<string>("mouse");
  const app = appName(appFor(pick.kind));
  const added = pick.request?.status === "added";
  const target = { kind: pick.kind, tmdbId: pick.tmdb_id, pickId: pick.id };
  const link = titleLink(target);

  const open = () => {
    setRevealed(false);
    openTitle(target);
  };

  // On touch, a tap elsewhere hides the revealed details again.
  useEffect(() => {
    if (!revealed) return;
    const onDown = (e: PointerEvent) => {
      if (!cardRef.current?.contains(e.target as Node)) setRevealed(false);
    };
    document.addEventListener("pointerdown", onDown);
    return () => document.removeEventListener("pointerdown", onDown);
  }, [revealed]);

  const onPosterClick = (e: MouseEvent<HTMLDivElement>) => {
    // Buttons and links act on their own; they must not open the details.
    if ((e.target as HTMLElement).closest("button, a")) return;
    // Touch has no hover: the first tap shows the quick actions, the next one opens the details.
    if (pointerType.current === "touch" && !revealed) {
      setRevealed(true);
      return;
    }
    open();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLElement>) => {
    if (e.key === "Enter" && e.target === e.currentTarget) {
      e.preventDefault();
      open();
    }
  };

  return (
    <article
      ref={cardRef}
      tabIndex={0}
      onKeyDown={onKeyDown}
      aria-label={`${pick.title}${pick.year ? ` (${pick.year})` : ""}, score ${pick.score}. Press Enter for details.`}
      className="group/card relative flex flex-col rounded-[var(--radius-poster)] focus-visible:outline-offset-4"
    >
      <div
        className="relative cursor-pointer"
        onPointerDown={(e) => {
          pointerType.current = e.pointerType;
        }}
        onClick={onPosterClick}
      >
        <Poster
          src={pick.poster_url}
          title={pick.title}
          className={cn(
            "transition-[filter,opacity] duration-300",
            pick.verdict === "ignored" && "opacity-55 grayscale",
          )}
        />

        <ScoreBadge
          score={pick.score}
          label={openSearch && ratingScore(pick.ratings) !== undefined ? "Score from IMDb and Rotten Tomatoes" : "Claude's match score"}
        />
        {pick.source === "free" && !openSearch && (
          <span className="absolute top-2.5 right-2.5 z-10 rounded-full bg-black/55 px-2 py-0.5 text-[11px] font-medium text-white/90 backdrop-blur-md">
            Outside the list
          </span>
        )}

        {/* State strip, always visible */}
        {(added || pick.verdict) && !revealed && (
          <div className="scrim-bottom absolute inset-x-0 bottom-0 flex items-end rounded-b-[var(--radius-poster)] px-3 pt-10 pb-2.5 text-[13px] text-white transition-opacity group-focus-within/card:opacity-0 group-hover/card:opacity-0">
            <VerdictLine pick={pick} app={app} />
          </div>
        )}

        {/* Details and actions: hover or focus on desktop, tap on touch */}
        <div
          className={cn(
            // pt-14 keeps the overview clear of the score badge, which stays on top.
            "absolute inset-0 flex flex-col justify-end rounded-[var(--radius-poster)] bg-[rgb(var(--scrim)/0.88)] p-3 pt-14 text-white opacity-0 backdrop-blur-[2px] transition-opacity duration-200",
            "group-focus-within/card:opacity-100 [@media(hover:hover)]:group-hover/card:opacity-100",
            revealed ? "opacity-100" : "pointer-events-none group-focus-within/card:pointer-events-auto [@media(hover:hover)]:group-hover/card:pointer-events-auto",
          )}
        >
          {pick.overview && <p className="line-clamp-[8] min-h-0 overflow-hidden text-[13px] leading-snug text-white/85">{pick.overview}</p>}
          {pick.genres && pick.genres.length > 0 && <p className="mt-2 line-clamp-1 text-xs text-white/60">{pick.genres.join(", ")}</p>}
          <p className="mt-1 flex items-center gap-1.5 text-xs text-white/80">
            <Tv className="size-3.5 shrink-0" />
            <span className="line-clamp-1">{pick.streaming && pick.streaming.length > 0 ? `Streams on ${pick.streaming.join(", ")}` : "Not on a streaming service here"}</span>
          </p>
          <TitleLinks pick={pick} tone="poster" className="mt-2" />
          <p className="mt-2 hidden text-[11px] text-white/55 [@media(hover:none)]:block">Tap again for details</p>

          <div className="mt-3 flex items-center gap-1.5">
            {added ? (
              <p className="flex h-8 flex-1 items-center gap-1.5 text-[13px] font-medium text-white">
                <Check className="size-4 text-[#7fd6bc]" /> In {app}
              </p>
            ) : (
              <Button variant="primary" size="sm" className="flex-1" onClick={() => onAccept(pick)}>
                <Plus /> Add
              </Button>
            )}
            {!added && pick.verdict !== "later" && (
              <Button variant="onPoster" size="iconSm" aria-label={`Save ${pick.title} for later`} title="Later" onClick={() => decide(pick, "later")}>
                <Clock />
              </Button>
            )}
            {!added && pick.verdict !== "ignored" && (
              <Button variant="onPoster" size="iconSm" aria-label={`Ignore ${pick.title}`} title="Ignore" onClick={() => decide(pick, "ignored")}>
                <EyeOff />
              </Button>
            )}
            {!added && (pick.verdict === "later" || pick.verdict === "ignored") && (
              <Button variant="onPoster" size="iconSm" aria-label={`Move ${pick.title} back to undecided`} title="Move back" onClick={() => decide(pick, "")}>
                <RotateCcw />
              </Button>
            )}
          </div>
        </div>
      </div>

      <div className="mt-3 flex min-w-0 flex-col gap-1.5 px-0.5">
        <h3 className="text-[15px] leading-tight font-semibold">
          {/* The card itself takes keyboard focus; the link serves pointers, new tabs and screen reader browsing. */}
          <Link to={link.to} state={link.state} tabIndex={-1} className="line-clamp-1 decoration-text-muted/60 underline-offset-4 hover:underline">
            {pick.title}
          </Link>
          {pick.year && <span className="nums text-[13px] font-normal text-text-muted">{pick.year}</span>}
        </h3>
        <RatingChips ratings={pick.ratings} />
        <p className="line-clamp-2 text-[13px] leading-snug text-text-muted">{pick.reason}</p>
        {pick.related_to && pick.related_to.length > 0 && (
          <ul className="mt-0.5 flex flex-wrap gap-1" aria-label={openSearch ? "Related titles" : "Related to your library"}>
            {pick.related_to.map((t) => (
              <li key={t} className="max-w-full truncate rounded-full border border-border px-2 py-0.5 text-[11px] text-text-muted">
                {t.replace(/\s\(\d{4}\)$/, "")}
              </li>
            ))}
          </ul>
        )}
      </div>
    </article>
  );
}

function ScoreBadge({ score, label }: { score: number; label: string }) {
  const high = score >= 90;
  return (
    <div
      className={cn(
        // Above the details overlay so the score and its tooltip stay available on hover.
        "absolute top-2.5 left-2.5 z-10 flex h-9 min-w-9 items-center justify-center rounded-[7px] px-1.5 font-display text-[22px] leading-none font-bold backdrop-blur-md",
        high ? "bg-accent text-accent-contrast" : "bg-black/60 text-white",
      )}
      role="img"
      aria-label={`${label}: ${score} of 100`}
      title={`${label}: ${score} of 100`}
    >
      <span className="nums">{score}</span>
    </div>
  );
}

function VerdictLine({ pick, app }: { pick: Pick; app: string }) {
  if (pick.request?.status === "added") {
    return (
      <span className="flex min-w-0 items-center gap-1.5">
        <Check className="size-3.5 shrink-0 text-[#7fd6bc]" />
        <span className="truncate">
          Added · {pick.request.quality_profile}
        </span>
        <span className="sr-only">to {app}</span>
      </span>
    );
  }
  if (pick.verdict === "later") {
    const until = shortDate(pick.later_until);
    return (
      <span className="flex items-center gap-1.5">
        <Clock className="size-3.5 shrink-0" />
        {until ? `Later, back on ${until}` : "Saved for later"}
      </span>
    );
  }
  if (pick.verdict === "ignored") {
    return (
      <span className="flex items-center gap-1.5">
        <EyeOff className="size-3.5 shrink-0" /> Ignored
      </span>
    );
  }
  if (pick.verdict === "accepted") {
    return (
      <span className="flex items-center gap-1.5">
        <Check className="size-3.5 shrink-0" /> Accepted, not added yet
      </span>
    );
  }
  return null;
}
