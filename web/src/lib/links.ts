interface Linkable {
  title: string;
  year?: number;
  imdb_id?: string;
}

/** The IMDb title page, or an IMDb search by title and year when the id is unknown. */
export function imdbUrl({ title, year, imdb_id }: Linkable): string {
  if (imdb_id && /^tt\d+$/.test(imdb_id)) return `https://www.imdb.com/title/${imdb_id}/`;
  return `https://www.imdb.com/find/?q=${encodeURIComponent(year ? `${title} ${year}` : title)}`;
}

/** The TMDB page for a movie or series. */
export function tmdbUrl(kind: "movies" | "series", tmdbId: number): string {
  return `https://www.themoviedb.org/${kind === "series" ? "tv" : "movie"}/${tmdbId}`;
}

/** Watching on YouTube itself; nothing loads until the link is followed. */
export function youtubeUrl(key: string): string {
  return `https://www.youtube.com/watch?v=${encodeURIComponent(key)}`;
}

/** Rotten Tomatoes has no id-based URL, so this is always a search. */
export function rottenTomatoesUrl({ title }: Linkable): string {
  return `https://www.rottentomatoes.com/search?search=${encodeURIComponent(title)}`;
}
