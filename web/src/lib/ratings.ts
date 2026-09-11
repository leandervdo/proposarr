import type { Pick, Ratings } from "@/api/types";

/**
 * The open-search rating score (docs/API.md): the mean of IMDb × 10 (with at least 1,000 votes)
 * and the Rotten Tomatoes critic score. Undefined when neither is known.
 */
export function ratingScore(ratings?: Ratings): number | undefined {
  const values: number[] = [];
  const imdb = ratings?.imdb;
  if (imdb && imdb.value > 0 && imdb.votes >= 1000) values.push(imdb.value * 10);
  if (typeof ratings?.rotten_tomatoes === "number") values.push(ratings.rotten_tomatoes);
  if (values.length === 0) return undefined;
  return values.reduce((a, b) => a + b, 0) / values.length;
}

/** Best rated first; picks without ratings after, by score. Stable for equal picks. */
export function byRating(a: Pick, b: Pick): number {
  const ra = ratingScore(a.ratings);
  const rb = ratingScore(b.ratings);
  if (ra !== undefined && rb !== undefined) return rb - ra || (b.ratings?.imdb?.votes ?? 0) - (a.ratings?.imdb?.votes ?? 0);
  if (ra !== undefined) return -1;
  if (rb !== undefined) return 1;
  return b.score - a.score;
}
