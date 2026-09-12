import { ArrowUpRight, Play } from "lucide-react";
import { useState } from "react";
import { youtubeUrl } from "@/lib/links";

/**
 * A play tile over the TMDB backdrop. Nothing is requested from YouTube until the tile is pressed; then the
 * privacy-enhanced embed replaces it inline.
 */
export function TitleTrailer({ trailer, backdropUrl, title }: { trailer: { name: string; youtube_key: string }; backdropUrl?: string; title: string }) {
  const [playing, setPlaying] = useState(false);
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  const showBackdrop = backdropUrl && failedSrc !== backdropUrl;
  const key = encodeURIComponent(trailer.youtube_key);

  return (
    <div>
      <div className="relative aspect-video w-full overflow-hidden rounded-[var(--radius-panel)] bg-[#0c0b10]">
        {playing ? (
          <iframe
            src={`https://www.youtube-nocookie.com/embed/${key}?autoplay=1`}
            title={`${trailer.name}: ${title}`}
            loading="lazy"
            allow="autoplay; encrypted-media; picture-in-picture; fullscreen"
            allowFullScreen
            referrerPolicy="strict-origin-when-cross-origin"
            className="absolute inset-0 size-full border-0"
          />
        ) : (
          <button
            type="button"
            onClick={() => setPlaying(true)}
            aria-label={`Play ${trailer.name}`}
            className="group absolute inset-0 block size-full text-left focus-visible:outline-offset-[-4px]"
          >
            {showBackdrop ? (
              <img
                src={backdropUrl}
                alt=""
                loading="lazy"
                decoding="async"
                onError={() => setFailedSrc(backdropUrl)}
                className="absolute inset-0 size-full object-cover opacity-75 transition-[opacity,scale] duration-500 group-hover:scale-[1.03] group-hover:opacity-90"
              />
            ) : (
              <span aria-hidden className="absolute inset-0 bg-[radial-gradient(circle_at_30%_20%,#3a2f1a_0%,#15141a_70%)]" />
            )}
            <span aria-hidden className="scrim-bottom absolute inset-0" />
            <span aria-hidden className="absolute inset-0 grid place-items-center">
              <span className="grid size-16 place-items-center rounded-full bg-accent text-accent-contrast shadow-[0_16px_40px_-8px_rgb(0_0_0/0.7)] transition-transform duration-200 group-hover:scale-110 sm:size-20">
                <Play className="ml-1 size-7 fill-current sm:size-8" />
              </span>
            </span>
            <span aria-hidden className="absolute inset-x-0 bottom-0 p-4 sm:p-5">
              <span className="block text-[11px] font-semibold tracking-[0.16em] text-white/65 uppercase">Trailer</span>
              <span className="mt-1 line-clamp-1 block font-display text-[22px] leading-none font-bold text-white sm:text-[28px]">{trailer.name}</span>
            </span>
          </button>
        )}
      </div>
      <p className="mt-2.5 flex justify-end text-[13px]">
        <a
          href={youtubeUrl(trailer.youtube_key)}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 font-medium text-text-muted underline decoration-border underline-offset-4 transition-colors hover:text-text"
        >
          Open on YouTube
          <ArrowUpRight aria-hidden className="size-3.5" />
        </a>
      </p>
    </div>
  );
}

export function TrailerSkeleton() {
  return <div aria-hidden className="aspect-video w-full animate-pulse rounded-[var(--radius-panel)] bg-surface-raised" />;
}
