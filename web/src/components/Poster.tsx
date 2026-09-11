import { useState } from "react";
import { initials } from "@/lib/format";
import { cn } from "@/lib/utils";

function hue(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) % 360;
  return h;
}

/** A 2:3 poster that falls back to a typographic tile when there is no image. */
export function Poster({ src, title, className, rounded = true }: { src?: string; title: string; className?: string; rounded?: boolean }) {
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  const showImage = src && failedSrc !== src;
  return (
    <div className={cn("relative aspect-[2/3] overflow-hidden bg-surface-raised", rounded && "rounded-[var(--radius-poster)]", className)}>
      {showImage ? (
        <img
          src={src}
          alt=""
          loading="lazy"
          decoding="async"
          onError={() => setFailedSrc(src)}
          className="absolute inset-0 size-full object-cover"
        />
      ) : (
        <InitialsTile title={title} />
      )}
      <div className="pointer-events-none absolute inset-0 rounded-[inherit] shadow-[inset_0_0_0_1px_rgb(255_255_255/0.06)]" />
    </div>
  );
}

function InitialsTile({ title }: { title: string }) {
  const h = hue(title);
  return (
    <div
      aria-hidden
      className="absolute inset-0 flex flex-col justify-end gap-[4cqw] p-[9%] [container-type:size]"
      style={{
        background: `linear-gradient(160deg, hsl(${h} 32% 26%) 0%, hsl(${(h + 24) % 360} 30% 14%) 100%)`,
      }}
    >
      <span className="font-display text-[42cqw] leading-[0.8] font-bold tracking-tight text-white/80">{initials(title)}</span>
      <span className="h-px w-1/3 bg-white/25" />
      <span className="line-clamp-3 font-display text-[12cqw] leading-[1.05] font-semibold text-white/75">{title}</span>
    </div>
  );
}
