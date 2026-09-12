import { useCallback, useEffect, useRef } from "react";
import { useLocation, useNavigate, useSearchParams, type To } from "react-router";
import type { Kind } from "@/api/types";

// The title modal lives in the URL: ?title=movies:329865&pick=42. Opening pushes a history entry, so Back
// closes it and the address can be shared.

export interface TitleTarget {
  kind: Kind;
  tmdbId: number;
  /** Set when the title was opened from a pick. */
  pickId?: number;
}

/** Router state on entries pushed by opening the modal, so closing can go back instead of pushing. */
export interface TitleModalState {
  titleModal: true;
}

export function parseTitleTarget(params: URLSearchParams): TitleTarget | null {
  const m = /^(movies|series):(\d+)$/.exec(params.get("title") ?? "");
  if (!m) return null;
  const pick = Number(params.get("pick"));
  return { kind: m[1] as Kind, tmdbId: Number(m[2]), pickId: Number.isInteger(pick) && pick > 0 ? pick : undefined };
}

function withTarget(search: string, target: TitleTarget): string {
  const params = new URLSearchParams(search);
  params.set("title", `${target.kind}:${target.tmdbId}`);
  if (target.pickId !== undefined) params.set("pick", String(target.pickId));
  else params.delete("pick");
  return `?${params}`;
}

/** Link props that open a title on the current page. */
export function useTitleLink() {
  const location = useLocation();
  return useCallback(
    (target: TitleTarget): { to: To; state: TitleModalState } => ({
      to: { pathname: location.pathname, search: withTarget(location.search, target) },
      state: { titleModal: true },
    }),
    [location.pathname, location.search],
  );
}

export function useOpenTitle() {
  const navigate = useNavigate();
  const link = useTitleLink();
  return useCallback(
    (target: TitleTarget) => {
      const { to, state } = link(target);
      navigate(to, { state });
    },
    [navigate, link],
  );
}

/** The open title and a close function that undoes the push, or strips the params on a shared link. */
export function useTitleModal() {
  const [params, setParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const target = parseTitleTarget(params);
  const pushed = (location.state as Partial<TitleModalState> | null)?.titleModal === true;
  const targetKey = target ? `${target.kind}:${target.tmdbId}:${target.pickId ?? ""}` : "";

  // Going back is asynchronous; a second Esc before it lands must not go back twice.
  const closing = useRef(false);
  useEffect(() => {
    closing.current = false;
  }, [targetKey]);

  const close = useCallback(() => {
    if (closing.current) return;
    closing.current = true;
    if (pushed) {
      navigate(-1);
      return;
    }
    setParams(
      (p) => {
        const next = new URLSearchParams(p);
        next.delete("title");
        next.delete("pick");
        return next;
      },
      { replace: true },
    );
  }, [pushed, navigate, setParams]);

  return { target, close };
}
