import { ChevronLeft, ChevronRight } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import type { CastMember } from "@/api/types";
import { initials } from "@/lib/format";
import { Button } from "./ui/button";

/** A horizontally scrolling cast row with round photos. `heading` is rendered next to the scroll buttons. */
export function TitleCast({ cast, heading, labelledBy }: { cast: CastMember[]; heading: ReactNode; labelledBy: string }) {
  const listRef = useRef<HTMLUListElement>(null);
  const [edges, setEdges] = useState({ start: true, end: true });

  useEffect(() => {
    const el = listRef.current;
    if (!el) return;
    const measure = () => setEdges({ start: el.scrollLeft <= 2, end: el.scrollLeft + el.clientWidth >= el.scrollWidth - 2 });
    // The observer reports the first size on its own, after layout.
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    el.addEventListener("scroll", measure, { passive: true });
    return () => {
      observer.disconnect();
      el.removeEventListener("scroll", measure);
    };
  }, []);

  const scroll = (direction: 1 | -1) => {
    const el = listRef.current;
    if (!el) return;
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    el.scrollBy({ left: direction * Math.max(el.clientWidth * 0.8, 120), behavior: reduce ? "auto" : "smooth" });
  };

  return (
    <section aria-labelledby={labelledBy}>
      <div className="flex items-center justify-between gap-4">
        {heading}
        {!(edges.start && edges.end) && (
          <div className="flex gap-1.5">
            <Button variant="secondary" size="iconSm" aria-label="Scroll cast back" disabled={edges.start} onClick={() => scroll(-1)}>
              <ChevronLeft />
            </Button>
            <Button variant="secondary" size="iconSm" aria-label="Scroll cast forward" disabled={edges.end} onClick={() => scroll(1)}>
              <ChevronRight />
            </Button>
          </div>
        )}
      </div>
      <ul
        ref={listRef}
        // overflow-y-hidden: with only overflow-x set, the row also scrolls vertically by a pixel and swallows the wheel.
        className="no-scrollbar -mx-5 mt-4 flex snap-x snap-mandatory scroll-px-5 gap-3 overflow-x-auto overflow-y-hidden px-5 pb-1 sm:-mx-8 sm:scroll-px-8 sm:gap-4 sm:px-8 lg:mx-0 lg:scroll-px-0 lg:px-0"
      >
        {cast.map((member, i) => (
          <li key={`${member.name}-${i}`} className="w-[5.75rem] shrink-0 snap-start text-center sm:w-[6.5rem]">
            <CastPhoto name={member.name} src={member.profile_url} />
            <p className="mt-2 line-clamp-2 text-[13px] leading-tight font-medium">{member.name}</p>
            {member.character && <p className="mt-0.5 line-clamp-2 text-xs leading-tight text-text-muted">{member.character}</p>}
          </li>
        ))}
      </ul>
    </section>
  );
}

function CastPhoto({ name, src }: { name: string; src?: string }) {
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  const showImage = src && failedSrc !== src;
  return (
    <div className="relative mx-auto size-[5.25rem] overflow-hidden rounded-full bg-surface-raised shadow-[inset_0_0_0_1px_var(--border)] sm:size-24">
      {showImage ? (
        <img
          src={src}
          alt=""
          loading="lazy"
          decoding="async"
          onError={() => setFailedSrc(src)}
          className="absolute inset-0 size-full object-cover object-[50%_20%]"
        />
      ) : (
        <span aria-hidden className="absolute inset-0 grid place-items-center font-display text-[28px] font-bold tracking-tight text-text-muted">
          {initials(name)}
        </span>
      )}
    </div>
  );
}

export function CastSkeleton() {
  return (
    <div aria-hidden>
      <div className="h-6 w-20 animate-pulse rounded bg-surface-raised" />
      <div className="mt-4 flex gap-4 overflow-hidden">
        {Array.from({ length: 7 }, (_, i) => (
          <div key={i} className="w-[5.75rem] shrink-0 sm:w-[6.5rem]">
            <div className="mx-auto size-[5.25rem] animate-pulse rounded-full bg-surface-raised sm:size-24" />
            <div className="mx-auto mt-2 h-3 w-4/5 animate-pulse rounded bg-surface-raised" />
            <div className="mx-auto mt-1.5 h-2.5 w-3/5 animate-pulse rounded bg-surface-raised" />
          </div>
        ))}
      </div>
    </div>
  );
}
