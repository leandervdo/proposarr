// In-memory backend for `VITE_MOCK=1 pnpm dev`. Add ?scenario=empty|down|unconfigured to the URL
// to see the empty, unreachable and not-configured screens.
import { ratingScore } from "@/lib/ratings";
import { ApiError } from "./client";
import type { LiveEvent } from "./events";
import type {
  AppConfig,
  AppOptions,
  CheckResult,
  Kind,
  Library,
  LibraryTitle,
  Pick,
  Profile,
  Run,
  Service,
  SettingField,
  SettingValues,
  Settings,
  Status,
  TestResult,
  Verdict,
} from "./types";

type Scenario = "default" | "empty" | "down" | "unconfigured" | "setup";

const scenario: Scenario = (() => {
  try {
    const fromUrl = new URLSearchParams(location.search).get("scenario");
    if (fromUrl) sessionStorage.setItem("proposarr.mock", fromUrl);
    return (sessionStorage.getItem("proposarr.mock") as Scenario) || "default";
  } catch {
    return "default";
  }
})();

const IMG = "https://image.tmdb.org/t/p/w500";
const now = Date.now();
const ago = (minutes: number) => new Date(now - minutes * 60_000).toISOString();
const inDays = (days: number) => new Date(now + days * 86_400_000).toISOString();
const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));

// ---------- fixtures ----------

type PickSeed = Omit<Pick, "id" | "run_id" | "kind">;

const moviePicks: PickSeed[] = [
  {
    tmdb_id: 329865, imdb_id: "tt2543164", title: "Arrival", year: 2016, score: 93, source: "candidate",
    ratings: { imdb: { value: 7.9, votes: 812_406 }, rotten_tomatoes: 94, metacritic: 81 },
    reason: "A quiet, cerebral first-contact story built around language and grief, the same register as Interstellar.",
    related_to: ["Interstellar (2014)", "Contact (1997)"], overview: "Taking place after alien crafts land around the world, an expert linguist is recruited by the military to determine whether they come in peace or are a threat.",
    genres: ["Drama", "Science Fiction", "Mystery"], rating: 7.6, streaming: ["Netflix"], poster_url: `${IMG}/x2FJsf1ElAgr63Y3PNPtJrcmpoe.jpg`,
  },
  {
    tmdb_id: 264660, imdb_id: "tt0470752", title: "Ex Machina", year: 2015, score: 90, source: "candidate",
    ratings: { imdb: { value: 7.7, votes: 603_118 }, rotten_tomatoes: 92, metacritic: 78 },
    reason: "Tense, contained sci-fi about consciousness that you rewatched Her for.",
    related_to: ["Her (2013)", "Blade Runner 2049 (2017)"], overview: "Caleb, a coder at an internet-search giant, wins a competition to spend a week at a private mountain retreat belonging to the CEO, where he takes part in a strange experiment.",
    genres: ["Drama", "Science Fiction"], rating: 7.6, streaming: ["Max", "Prime Video"], poster_url: `${IMG}/dmJW8IAKHKxFNiUnoDR7JfsK7Rp.jpg`,
    verdict: "accepted", verdict_at: ago(50),
    request: { pick_id: 0, app: "radarr", target_id: 412, quality_profile: "Remux + WEB 2160p", root_folder: "/media/movies", status: "added", requested_at: ago(50) },
  },
  {
    tmdb_id: 300668, imdb_id: "tt2798920", title: "Annihilation", year: 2018, score: 88, source: "candidate",
    ratings: { imdb: { value: 6.8, votes: 379_552 }, rotten_tomatoes: 88, metacritic: 79 },
    reason: "Hypnotic, unsettling sci-fi with the slow dread you liked in Arrival's director's work.",
    related_to: ["Blade Runner 2049 (2017)", "Sicario (2015)"], overview: "A biologist signs up for a dangerous, secret expedition into a mysterious zone where the laws of nature don't apply.",
    genres: ["Science Fiction", "Horror", "Mystery"], rating: 6.4, streaming: ["Paramount+"], poster_url: `${IMG}/d3qcpfNwbAMCNqWDHzPQsUYiUgS.jpg`,
  },
  {
    // No imdb_id and no ratings: exercises the IMDb search link and the missing chips.
    tmdb_id: 17431, title: "Moon", year: 2009, score: 86, source: "candidate",
    reason: "A one-man, low-budget character study that rewards the same patience as The Martian's quieter scenes.",
    related_to: ["The Martian (2015)"], overview: "With only three weeks left in his three year contract, Sam Bell is getting anxious to finally return to Earth.",
    genres: ["Science Fiction", "Drama"], rating: 7.6, streaming: [],
  },
  {
    tmdb_id: 666277, imdb_id: "tt13238346", title: "Past Lives", year: 2023, score: 85, source: "free",
    ratings: { imdb: { value: 7.8, votes: 118_904 }, rotten_tomatoes: 95, metacritic: 94 },
    reason: "Tender, adult drama about paths not taken, close to the feeling of Her without the science fiction.",
    related_to: ["Her (2013)"], overview: "Nora and Hae Sung, two childhood friends, are reunited in New York for one fateful week as they confront notions of destiny, love, and the choices that make a life.",
    genres: ["Drama", "Romance"], rating: 7.8, streaming: ["Paramount+"], poster_url: `${IMG}/k3waqVXSnvCZWfJYNtdamTgTtTA.jpg`,
    verdict: "later", verdict_at: ago(300), later_until: inDays(29),
  },
  {
    tmdb_id: 545611, imdb_id: "tt6710474", title: "Everything Everywhere All at Once", year: 2022, score: 84, source: "candidate",
    ratings: { imdb: { value: 7.8, votes: 597_215 }, rotten_tomatoes: 94, metacritic: 81 },
    reason: "Maximalist multiverse chaos with a real emotional core, for the part of you that rewatched Parasite.",
    related_to: ["Parasite (2019)"], overview: "An aging Chinese immigrant is swept up in an insane adventure, where she alone can save what's important to her by connecting with the lives she could have led in other universes.",
    genres: ["Action", "Adventure", "Science Fiction"], rating: 7.8, streaming: ["Prime Video"], poster_url: `${IMG}/u68AjlvlutfEIcpmbYpKcdi09ut.jpg`,
  },
  {
    tmdb_id: 1124, imdb_id: "tt0482571", title: "The Prestige", year: 2006, score: 83, source: "candidate",
    ratings: { imdb: { value: 8.5, votes: 1_468_730 }, rotten_tomatoes: 76, metacritic: 66 },
    reason: "An obsessive puzzle-box rivalry from the director of Interstellar and Oppenheimer.",
    related_to: ["Interstellar (2014)", "Oppenheimer (2023)"], overview: "A mysterious story of two magicians whose intense rivalry leads them on a life-long battle for supremacy.",
    genres: ["Drama", "Mystery", "Science Fiction"], rating: 8.2, streaming: ["Netflix"], poster_url: `${IMG}/bdN3gXuIZYaJP7ftKK2sU0nPtEA.jpg`,
  },
  {
    tmdb_id: 9693, imdb_id: "tt0206634", title: "Children of Men", year: 2006, score: 82, source: "candidate",
    ratings: { imdb: { value: 7.9, votes: 571_287 }, rotten_tomatoes: 92, metacritic: 84 },
    reason: "Grounded, bleak near-future filmmaking with long takes that match the tension of Sicario.",
    related_to: ["Sicario (2015)"], overview: "In 2027, in a chaotic world in which humans can no longer procreate, a former activist agrees to help transport a miraculously pregnant woman to a sanctuary at sea.",
    genres: ["Drama", "Action", "Thriller", "Science Fiction"], rating: 7.6, streaming: ["Peacock"],
    verdict: "ignored", verdict_at: ago(200),
  },
  {
    tmdb_id: 152601, imdb_id: "tt1798709", title: "Her", year: 2013, score: 79, source: "candidate",
    ratings: { imdb: { value: 8.0, votes: 694_512 }, rotten_tomatoes: 95, metacritic: 91 },
    reason: "Soft-spoken story about loneliness and technology that pairs with Ex Machina.",
    related_to: ["Ex Machina (2015)"], overview: "In the not so distant future, Theodore, a lonely writer, purchases a newly developed operating system designed to meet the user's every need.",
    genres: ["Romance", "Science Fiction", "Drama"], rating: 7.9, streaming: ["Netflix"], poster_url: `${IMG}/eCOtqtfvn7mxGl6nfmq4b1exJRc.jpg`,
  },
  {
    tmdb_id: 603, imdb_id: "tt0133093", title: "The Matrix", year: 1999, score: 77, source: "candidate",
    ratings: { imdb: { value: 8.7, votes: 2_275_363 }, rotten_tomatoes: 83, metacritic: 73 },
    reason: "A classic worth owning if you rewatched Blade Runner 2049 twice this year.",
    related_to: ["Blade Runner 2049 (2017)"], overview: "Set in the 22nd century, The Matrix tells the story of a computer hacker who joins a group of underground insurgents fighting the vast and powerful computers who now rule the earth.",
    genres: ["Action", "Science Fiction"], rating: 8.2, streaming: ["Max"], poster_url: `${IMG}/f89U3ADr1oiB1s9GkdPOEpXUk5H.jpg`,
  },
];

const seriesPicks: PickSeed[] = [
  {
    tmdb_id: 70523, imdb_id: "tt5753856", title: "Dark", year: 2017, score: 94, source: "candidate",
    ratings: { imdb: { value: 8.7, votes: 468_992 } },
    reason: "Intricate time-loop mystery for the viewer who finished Severance in two weekends.",
    related_to: ["Severance (2022)", "Mr. Robot (2015)"], overview: "A missing child causes four families to help each other for answers. What they could not imagine is that this mystery would be connected to innovations from three generations.",
    genres: ["Crime", "Drama", "Sci-Fi & Fantasy", "Mystery"], rating: 8.4, streaming: ["Netflix"], poster_url: `${IMG}/apbrbWs8M9lyOpJYU5WXrpFbk1Z.jpg`,
  },
  {
    tmdb_id: 87108, imdb_id: "tt7366338", title: "Chernobyl", year: 2019, score: 92, source: "candidate",
    ratings: { imdb: { value: 9.3, votes: 905_310 } },
    reason: "A complete five-episode drama with the procedural dread you liked in True Detective.",
    related_to: ["True Detective (2014)"], overview: "The true story of one of the worst man-made catastrophes in history: the catastrophic nuclear accident at Chernobyl.",
    genres: ["Drama"], rating: 8.7, streaming: ["Max"], poster_url: `${IMG}/hlLXt2tOPT6RRnjiUmoxyG1LTFi.jpg`,
    verdict: "accepted", verdict_at: ago(1500),
    request: { pick_id: 0, app: "sonarr", target_id: 88, quality_profile: "Remux + WEB 1080p", root_folder: "/media/series", status: "added", requested_at: ago(1500) },
  },
  {
    tmdb_id: 126308, imdb_id: "tt2788316", title: "Shōgun", year: 2024, score: 90, source: "candidate",
    ratings: { imdb: { value: 8.6, votes: 231_447 } },
    reason: "Prestige historical epic with the political scheming of Andor.",
    related_to: ["Andor (2022)"], overview: "In Japan in the year 1600, at the dawn of a century-defining civil war, Lord Yoshii Toranaga is fighting for his life as his enemies unite against him.",
    genres: ["Drama", "War & Politics"], rating: 8.6, streaming: ["Disney+"], poster_url: `${IMG}/7O4iVfOMQmdCSxhOg1WnzG1AgYT.jpg`,
  },
  {
    tmdb_id: 90660, imdb_id: "tt10574236", title: "Station Eleven", year: 2021, score: 87, source: "free",
    ratings: { imdb: { value: 7.5, votes: 47_806 } },
    reason: "A hopeful, finished post-pandemic story that shares The Leftovers' interest in grief.",
    related_to: ["The Leftovers (2014)"], overview: "A post-apocalyptic saga spanning multiple timelines, telling the stories of survivors of a devastating flu as they attempt to rebuild and reimagine the world anew.",
    genres: ["Drama", "Sci-Fi & Fantasy"], rating: 7.6, streaming: ["Max"],
  },
  {
    tmdb_id: 60622, imdb_id: "tt2802850", title: "Fargo", year: 2014, score: 85, source: "candidate",
    ratings: { imdb: { value: 8.9, votes: 441_205 } },
    reason: "Anthology crime with dark humour, each season complete, like True Detective.",
    related_to: ["True Detective (2014)"], overview: "A close-knit anthology series dealing with stories involving malice, violence and murder based in and around Minnesota.",
    genres: ["Crime", "Drama"], rating: 8.3, streaming: ["Hulu"],
    verdict: "later", verdict_at: ago(1400), later_until: inDays(12),
  },
];

type SearchSeed = Omit<PickSeed, "score" | "source">;

// Open search ("use my taste" off): free picks, ranked by the rating score, mostly without related titles.
const searchSeeds: Record<Kind, SearchSeed[]> = {
  movies: [
    {
      tmdb_id: 629, imdb_id: "tt0114814", title: "The Usual Suspects", year: 1995,
      reason: "A police-station interrogation unravels a botched job, and the last minute rewrites everything before it.",
      related_to: [], overview: "Held in an L.A. interrogation room, Verbal Kint attempts to convince the feds that a mythic crime lord, Keyser Soze, not only exists, but was also responsible for drawing him and his four partners into a multi-million dollar heist.",
      genres: ["Drama", "Crime", "Thriller"], rating: 8.2, streaming: ["Prime Video"],
      ratings: { imdb: { value: 8.5, votes: 1_196_402 }, rotten_tomatoes: 89, metacritic: 77 },
    },
    {
      tmdb_id: 500, imdb_id: "tt0105236", title: "Reservoir Dogs", year: 1992,
      reason: "The heist happens off screen; the twist is who in the warehouse is not what he claims.",
      related_to: [], overview: "A botched robbery indicates a police informant, and the pressure mounts in the aftermath at a warehouse. Crime begets violence as the survivors unravel their mistakes.",
      genres: ["Crime", "Thriller"], rating: 8.1, streaming: ["Netflix"],
      ratings: { imdb: { value: 8.3, votes: 1_121_873 }, rotten_tomatoes: 90, metacritic: 79 },
    },
    {
      tmdb_id: 1389, imdb_id: "tt0120780", title: "Out of Sight", year: 1998,
      reason: "A charming prison-break-to-diamond-job caper whose double crosses land in the final act.",
      related_to: ["Heat (1995)"], overview: "Meet Jack Foley, a smooth criminal who bends the law and is determined to make one last heist. Karen Sisco is a federal marshal who chooses all the right moves… and all the wrong guys.",
      genres: ["Comedy", "Crime", "Romance"], rating: 6.8, streaming: [],
      ratings: { imdb: { value: 7.0, votes: 104_331 }, rotten_tomatoes: 93, metacritic: 85 },
    },
    {
      tmdb_id: 100, imdb_id: "tt0120735", title: "Lock, Stock and Two Smoking Barrels", year: 1998,
      reason: "Four friends, a rigged card game and a robbery that loops back on everyone in one last reveal.",
      related_to: [], overview: "A card shark and his unwillingly-enlisted friends need to make a lot of cash quick after losing a sketchy poker match.",
      genres: ["Comedy", "Crime"], rating: 8.1, streaming: ["Max"],
      ratings: { imdb: { value: 8.1, votes: 623_950 }, rotten_tomatoes: 76, metacritic: 66 },
    },
    {
      tmdb_id: 2322, imdb_id: "tt0105435", title: "Sneakers", year: 1992,
      reason: "A security-testing crew pulls a job for the wrong client, with a clever switch at the end.",
      related_to: [], overview: "When shadowy U.S. intelligence agents blackmail a reformed computer hacker and his eccentric team of security experts into stealing a code-breaking 'black box' from a Soviet-funded genius, they uncover a bigger conspiracy.",
      genres: ["Crime", "Drama", "Thriller"], rating: 7.0, streaming: ["Peacock"],
      ratings: { imdb: { value: 7.1, votes: 71_624 }, rotten_tomatoes: 80, metacritic: 65 },
    },
    {
      tmdb_id: 8195, imdb_id: "tt0122690", title: "Ronin", year: 1998,
      reason: "Mercenaries chase a briefcase through France, and the question of who hired them pays off late.",
      related_to: [], overview: "A briefcase with undisclosed contents, sought by Irish terrorists and the Russian mob, makes its way into the hands of a mysterious woman.",
      genres: ["Action", "Crime", "Thriller"], rating: 7.1, streaming: [],
      ratings: { imdb: { value: 7.2, votes: 231_012 }, rotten_tomatoes: 69, metacritic: 67 },
    },
    {
      tmdb_id: 913, imdb_id: "tt0155267", title: "The Thomas Crown Affair", year: 1999,
      reason: "A bored billionaire steals a Monet for fun, and the museum finale flips the cat-and-mouse game.",
      related_to: [], overview: "A very rich and successful playboy amuses himself by stealing artwork, but may have met his match in a seductive detective.",
      genres: ["Romance", "Crime", "Thriller"], rating: 6.6, streaming: ["Prime Video"],
      ratings: { imdb: { value: 6.8, votes: 108_227 }, rotten_tomatoes: 69, metacritic: 73 },
    },
    {
      tmdb_id: 1844, imdb_id: "tt0137494", title: "Entrapment", year: 1999,
      reason: "An insurance investigator and a thief plan a millennium-eve bank job; loyalties shift at the last step.",
      related_to: [], overview: "Insurance agent Virginia Baker goes undercover to catch master thief Robert MacDougal, and the two end up planning one last theft together.",
      genres: ["Thriller", "Crime"], rating: 6.2, streaming: [],
    },
  ],
  series: [
    {
      tmdb_id: 93405, imdb_id: "tt10919420", title: "Squid Game", year: 2021,
      reason: "Nine tense episodes of deadly children's games with a reveal about who runs them.",
      related_to: [], overview: "Hundreds of cash-strapped players accept a strange invitation to compete in children's games. Inside, a tempting prize awaits — with deadly high stakes.",
      genres: ["Action & Adventure", "Mystery", "Drama"], rating: 7.8, streaming: ["Netflix"],
      ratings: { imdb: { value: 8.0, votes: 612_540 } },
    },
    {
      tmdb_id: 70593, imdb_id: "tt6611916", title: "Kingdom", year: 2019,
      reason: "A Joseon-era political thriller with a plague, six episodes a season.",
      related_to: [], overview: "While strange rumors about their ill king grip a kingdom, the crown prince becomes their only hope against a mysterious plague overtaking the land.",
      genres: ["Drama", "Mystery", "Action & Adventure"], rating: 8.1, streaming: ["Netflix"],
      ratings: { imdb: { value: 8.3, votes: 64_018 } },
    },
    {
      tmdb_id: 99966, imdb_id: "tt14169960", title: "All of Us Are Dead", year: 2022,
      reason: "A high school under siege, short enough for a long weekend.",
      related_to: [], overview: "A high school becomes ground zero for a zombie virus outbreak. Trapped students must fight their way out — or turn into one of the rabid infected.",
      genres: ["Action & Adventure", "Drama", "Sci-Fi & Fantasy"], rating: 8.2, streaming: ["Netflix"],
      ratings: { imdb: { value: 7.5, votes: 67_730 } },
    },
  ],
};

/** Scores and orders search seeds the way the server does for an open search. */
function searchPicks(kind: Kind): PickSeed[] {
  return searchSeeds[kind]
    .map((s) => ({ ...s, source: "free" as const, score: Math.round(ratingScore(s.ratings) ?? 55) }))
    .sort((a, b) => b.score - a.score);
}

const runs: Run[] = [];
const picks: Pick[] = [];
let nextRunId = 1;
let nextPickId = 1;

function makeRun(kind: Kind, over: Partial<Run>): Run {
  return {
    id: nextRunId++, kind, use_taste: true, model: "claude-sonnet-5", effort: "medium", status: "succeeded",
    started_at: ago(60), finished_at: ago(58), cost_usd: 0.1842, input_tokens: 48_210, output_tokens: 3_904,
    num_turns: 2, session_id: "a3f1c2d4-5b6e-4f70-8a91-b2c3d4e5f607", library_count: kind === "movies" ? 185 : 24,
    history_count: kind === "movies" ? 61 : 14, candidate_count: kind === "movies" ? 150 : 96, pick_count: 0,
    warnings: [], rejected: [], ...over,
  };
}

function addPicks(run: Run, seeds: PickSeed[]) {
  for (const s of seeds) {
    const id = nextPickId++;
    picks.push({ ...s, id, run_id: run.id, kind: run.kind, request: s.request ? { ...s.request, pick_id: id } : undefined });
  }
  run.pick_count = seeds.length;
}

if (scenario === "default") {
  const r1 = makeRun("movies", { started_at: ago(60 * 24 * 9), finished_at: ago(60 * 24 * 9 - 3), vibe: "something to watch with my parents", cost_usd: 0.1511 });
  addPicks(r1, moviePicks.slice(6).map((p) => ({ ...p, verdict: undefined, request: undefined })));
  const r2 = makeRun("series", {
    status: "failed", started_at: ago(60 * 24 * 4), finished_at: ago(60 * 24 * 4 - 1), cost_usd: 0, input_tokens: 0, output_tokens: 0, num_turns: 0,
    error: "TMDB: GET /3/tv/95396/recommendations: HTTP 401: Invalid API key: You must be granted a valid key.", candidate_count: 0,
  });
  const r3 = makeRun("movies", {
    status: "rate_limited", started_at: ago(60 * 26), finished_at: ago(60 * 26 - 1), cost_usd: 0, input_tokens: 0, output_tokens: 0, num_turns: 1,
    error: "claude session limit: You've hit your session limit · resets 11pm",
  });
  const r4 = makeRun("series", {
    started_at: ago(60 * 22), finished_at: ago(60 * 22 - 4), cost_usd: 0.2203, input_tokens: 39_872, output_tokens: 4_410,
    warnings: ["plex history failed: GET /status/sessions/history/all: context deadline exceeded"],
    rejected: [{ tmdb_id: 1399, title: "Game of Thrones", reason: "already in library or watch history" }],
  });
  addPicks(r4, seriesPicks);
  const search = makeRun("movies", {
    use_taste: false, vibe: "90s heist movies with a twist ending", started_at: ago(60 * 3), finished_at: ago(60 * 3 - 2),
    cost_usd: 0.0934, input_tokens: 14_380, output_tokens: 2_915, history_count: 0, candidate_count: 0,
    rejected: [
      { tmdb_id: 949, title: "Heat", reason: "already in library or watch history" },
      { title: "The Big Hit (1998)", reason: "not found on TMDB" },
    ],
  });
  addPicks(search, searchPicks("movies"));
  const r5 = makeRun("movies", {
    started_at: ago(95), finished_at: ago(92), vibe: "slow-burn sci-fi with a big idea", cost_usd: 0.1842,
    warnings: ["tmdb details failed for 2 of 38 watched titles: GET /3/movie/0: HTTP 404"],
    rejected: [
      { tmdb_id: 157336, title: "Interstellar", reason: "already in library or watch history" },
      { title: "Solaris (1972)", reason: "free pick limit reached" },
    ],
  });
  addPicks(r5, moviePicks);
  runs.push(r1, r2, r3, r4, search, r5);
  // A series run in progress when the page loads.
  startMockRun("series", "prestige drama I can finish in a month", 45_000);
}

// ---------- live events ----------

const listeners = new Set<(e: LiveEvent) => void>();
function emit(e: LiveEvent) {
  for (const l of listeners) l(e);
}

export function mockSubscribe(listener: (e: LiveEvent) => void) {
  listeners.add(listener);
  // Replay progress so a freshly mounted subscriber sees running runs.
  for (const r of runs.filter((r) => r.status === "running")) {
    setTimeout(() => listener({ type: "run.progress", data: { run_id: r.id, kind: r.kind, message: progressFor.get(r.id) ?? "Starting…" } }), 0);
  }
  return () => listeners.delete(listener);
}

export const mockConnected = () => scenario !== "down";

const progressFor = new Map<number, string>();

function startMockRun(kind: Kind, vibe: string, totalMs = 14_000, useTaste = true): Run {
  const run = makeRun(kind, {
    status: "running", use_taste: useTaste, started_at: new Date().toISOString(), finished_at: undefined, vibe: vibe || undefined,
    cost_usd: 0, input_tokens: 0, output_tokens: 0, num_turns: 0, ...(useTaste ? {} : { history_count: 0, candidate_count: 0 }),
  });
  runs.push(run);
  const library = `Loading ${kind === "series" ? "Sonarr" : "Radarr"} library…`;
  const steps = useTaste
    ? [library, "Reading Plex history…", "Gathering candidates from TMDB…", "Asking Claude (claude-sonnet-5, medium)…", "Verifying picks…"]
    : [library, "Asking Claude (claude-sonnet-5, medium) to search…", "Checking titles on TMDB…", "Ranking by IMDb and Rotten Tomatoes…"];
  const stepMs = totalMs / (steps.length + 1);
  steps.forEach((message, i) =>
    setTimeout(() => {
      progressFor.set(run.id, message);
      emit({ type: "run.progress", data: { run_id: run.id, kind, message } });
    }, stepMs * i + 300),
  );
  setTimeout(() => {
    Object.assign(run, { status: "succeeded", finished_at: new Date().toISOString(), cost_usd: 0.1733, input_tokens: 45_120, output_tokens: 3_610, num_turns: 2 });
    const seeds = useTaste
      ? (kind === "series" ? seriesPicks : moviePicks).map((p) => ({ ...p, verdict: undefined, verdict_at: undefined, later_until: undefined, request: undefined, score: Math.max(60, p.score - Math.round(Math.random() * 6)) }))
      : searchPicks(kind);
    addPicks(run, seeds);
    progressFor.delete(run.id);
    emit({ type: "run.finished", data: { ...run } });
  }, totalMs);
  queueMicrotask(() => emit({ type: "run.started", data: { ...run } }));
  return run;
}

// ---------- library ----------

const libraryMovies: LibraryTitle[] = [
  ["Interstellar", 2014, 157336, "/gEU2QniE6E77NI6lCU6MxlNBvIx.jpg", ["Adventure", "Drama", "Science Fiction"]],
  ["Blade Runner 2049", 2017, 335984, "/gajva2L0rPYkEWjzgFlBXCAVBE5.jpg", ["Science Fiction", "Drama"]],
  ["Dune", 2021, 438631, "/d5NXSklXo0qyIYkgV94XAgMIckC.jpg", ["Science Fiction", "Adventure"]],
  ["Sicario", 2015, 273481, "/lz8vNyXeidqqOdJW9ZjnDAMb5Vr.jpg", ["Action", "Crime", "Thriller"]],
  ["The Martian", 2015, 286217, "/5BHuvQ6p9kfc091Z8RiFNhCwL4b.jpg", ["Drama", "Adventure", "Science Fiction"]],
  ["Oppenheimer", 2023, 872585, "/8Gxv8gSFCU0XGDykEGv7zR1n2ua.jpg", ["Drama", "History"]],
  ["Parasite", 2019, 496243, "/7IiTTgloJzvGI1TAYymCfbfl3vT.jpg", ["Comedy", "Thriller", "Drama"]],
  ["Contact", 1997, 686, "", ["Drama", "Science Fiction", "Mystery"]],
  ["Prisoners", 2013, 146233, "", ["Drama", "Thriller", "Crime"]],
  ["Heat", 1995, 949, "", ["Crime", "Drama", "Action"]],
  ["Zodiac", 2007, 1949, "", ["Crime", "Drama", "Mystery"]],
  ["Gravity", 2013, 49047, "", ["Science Fiction", "Thriller", "Drama"]],
].map(([title, year, tmdb_id, poster, genres], i) => ({
  title: title as string, year: year as number, tmdb_id: tmdb_id as number, genres: genres as string[],
  poster_url: poster ? `${IMG}${poster}` : undefined, added: ago(60 * 24 * (i * 17 + 3)),
}));

const librarySeries: LibraryTitle[] = [
  ["Severance", 2022, 95396, ["Drama", "Mystery", "Sci-Fi & Fantasy"]],
  ["Mr. Robot", 2015, 62560, ["Crime", "Drama"]],
  ["True Detective", 2014, 46648, ["Drama", "Crime", "Mystery"]],
  ["The Leftovers", 2014, 54344, ["Drama", "Mystery"]],
  ["Andor", 2022, 83867, ["Sci-Fi & Fantasy", "Action & Adventure", "Drama"]],
  ["Game of Thrones", 2011, 1399, ["Sci-Fi & Fantasy", "Drama"]],
].map(([title, year, tmdb_id, genres], i) => ({
  title: title as string, year: year as number, tmdb_id: tmdb_id as number, genres: genres as string[], added: ago(60 * 24 * (i * 40 + 10)),
}));

function profileFor(kind: Kind): Profile {
  const zero = "0001-01-01T00:00:00Z";
  const top =
    kind === "movies"
      ? ([
          ["Blade Runner 2049", 2017, 4, "rewatched", true, ago(60 * 24 * 6)],
          ["Interstellar", 2014, 4, "rewatched", true, ago(60 * 24 * 21)],
          ["Her", 2013, 3, "watched", false, ago(60 * 24 * 12)],
          ["Sicario", 2015, 3, "watched", true, ago(60 * 24 * 33)],
          ["Parasite", 2019, 3, "watched", true, ago(60 * 24 * 48)],
          ["Oppenheimer", 2023, 3, "watched", true, ago(60 * 24 * 70)],
          ["The Martian", 2015, 1, "partial", true, ago(60 * 24 * 90)],
          ["Dune", 2021, 0.5, "owned", true, zero],
          ["Contact", 1997, 0.5, "owned", true, zero],
        ] as const)
      : ([
          ["Severance", 2022, 4, "rewatched", true, ago(60 * 24 * 4)],
          ["True Detective", 2014, 3, "watched", true, ago(60 * 24 * 30)],
          ["Andor", 2022, 3, "watched", true, ago(60 * 24 * 60)],
          ["The Leftovers", 2014, 1, "partial", true, ago(60 * 24 * 100)],
          ["Mr. Robot", 2015, 0.5, "owned", true, zero],
        ] as const);
  return {
    kind,
    top: top.map(([title, year, weight, signal, in_library, last_watched]) => ({ title, year, weight, signal, in_library, last_watched, added: zero })),
    genres:
      kind === "movies"
        ? [
            { name: "Science Fiction", share: 0.31 }, { name: "Drama", share: 0.27 }, { name: "Thriller", share: 0.12 },
            { name: "Adventure", share: 0.1 }, { name: "Crime", share: 0.08 }, { name: "Mystery", share: 0.06 },
            { name: "History", share: 0.04 }, { name: "Comedy", share: 0.02 },
          ]
        : [
            { name: "Drama", share: 0.38 }, { name: "Mystery", share: 0.22 }, { name: "Crime", share: 0.16 },
            { name: "Sci-Fi & Fantasy", share: 0.14 }, { name: "Action & Adventure", share: 0.1 },
          ],
    library_count: kind === "movies" ? 185 : 24,
    history_count: kind === "movies" ? 61 : 14,
  };
}

// ---------- settings ----------

type FieldSpec = { value?: string | number; secret?: boolean; kind?: "int" | "float" | "url" | "duration" | "enum"; min?: number; max?: number; options?: string[] };

const SPECS: Record<string, FieldSpec> = {
  "radarr.url": { kind: "url" }, "radarr.api_key": { secret: true }, "radarr.root_folder": {},
  "radarr.minimum_availability": { value: "released", kind: "enum", options: ["announced", "inCinemas", "released"] },
  "sonarr.url": { kind: "url" }, "sonarr.api_key": { secret: true }, "sonarr.root_folder": {},
  "plex.url": { kind: "url" }, "plex.token": { secret: true },
  "jellyfin.url": { kind: "url" }, "jellyfin.api_key": { secret: true }, "jellyfin.user_id": {},
  "tmdb.api_key": { secret: true }, "tmdb.region": { value: "US" },
  "claude.oauth_token": { secret: true }, "claude.api_key": { secret: true },
  "claude.timeout": { value: "10m", kind: "duration" }, "claude.max_budget_usd": { value: 0, kind: "float", min: 0 },
  history_days: { value: 180, kind: "int", min: 1, max: 3650 }, snapshot_ttl: { value: "6h", kind: "duration" },
  ...Object.fromEntries(
    (["movies", "series"] as const).flatMap((k) => [
      [`${k}.model`, { value: "claude-sonnet-5" }],
      [`${k}.effort`, { value: "medium", kind: "enum", options: ["low", "medium", "high", "xhigh", "max"] }],
      [`${k}.picks`, { value: 10, kind: "int", min: 1, max: 50 }],
      [`${k}.candidates`, { value: 150, kind: "int", min: 10, max: 500 }],
      [`${k}.free_picks`, { value: 3, kind: "int", min: 0, max: 10 }],
      [`${k}.seeds`, { value: 15, kind: "int", min: 1, max: 50 }],
      [`${k}.top_titles`, { value: 500, kind: "int", min: 5, max: 1000 }],
    ]),
  ),
};

function envName(key: string) {
  if (key === "claude.oauth_token") return "CLAUDE_CODE_OAUTH_TOKEN";
  if (key === "claude.api_key") return "ANTHROPIC_API_KEY";
  return `PROPOSARR_${key.replace(".", "_").toUpperCase()}`;
}

function defaultField(key: string): SettingField {
  const spec = SPECS[key]!;
  const hasDefault = spec.value !== undefined && spec.value !== "";
  return { value: spec.secret ? undefined : (spec.value ?? ""), secret: !!spec.secret, set: hasDefault, source: "default", locked: false, env: envName(key) };
}

const fields: Record<string, SettingField> = Object.fromEntries(Object.keys(SPECS).map((k) => [k, defaultField(k)]));

function put(key: string, over: Partial<SettingField>) {
  fields[key] = { ...fields[key]!, set: true, ...over };
}

if (scenario !== "setup") {
  put("radarr.url", { value: "http://192.168.1.238:7878", source: "ui" });
  put("radarr.api_key", { source: "default", hint: "read from initialize.json" });
  put("sonarr.url", { value: "http://192.168.1.238:8989", source: "env", locked: true });
  put("plex.url", { value: "http://192.168.1.238:32400", source: "ui" });
  put("plex.token", { source: "ui" });
  put("tmdb.api_key", { source: "file", locked: true });
  put("tmdb.region", { value: "NL", source: "ui" });
  put("claude.oauth_token", { source: "ui" });
  put("movies.effort", { value: "high", source: "ui" });
  put("series.model", { value: "claude-opus-5", source: "env", locked: true });
}

function missing(): string[] {
  const m: string[] = [];
  if (!fields["radarr.url"]!.set && !fields["sonarr.url"]!.set) m.push("radarr.url or sonarr.url");
  if (!fields["tmdb.api_key"]!.set) m.push("tmdb.api_key");
  if (scenario === "setup" && !fields["claude.oauth_token"]!.set && !fields["claude.api_key"]!.set) m.push("claude.oauth_token or claude.api_key");
  return m;
}

function settingsResponse(): Settings {
  return {
    fields: structuredClone(fields),
    read_only: { listen: ":8585", data_dir: "/config/data", claude_bin: "claude", web_auth: false, config_file: scenario === "setup" ? undefined : "/config/proposarr.yaml" },
  };
}

function validate(key: string, value: string | number): string | null {
  const spec = SPECS[key]!;
  const s = String(value).trim();
  switch (spec.kind) {
    case "url":
      return /^https?:\/\/[^\s/]+/.test(s) ? null : "Enter a full address starting with http:// or https://";
    case "int": {
      const n = Number(s);
      if (!Number.isInteger(n)) return "Enter a whole number";
      if (n < spec.min! || n > spec.max!) return `Enter a number from ${spec.min} to ${spec.max}`;
      return null;
    }
    case "float":
      return Number.isFinite(Number(s)) && Number(s) >= (spec.min ?? 0) ? null : "Enter a number of 0 or more";
    case "duration":
      return /^(\d+(h|m|s))+$/.test(s) ? null : "Use a duration like 10m or 6h";
    case "enum":
      return spec.options!.includes(s) ? null : `Choose one of ${spec.options!.join(", ")}`;
  }
  if (key === "tmdb.region" && !/^[A-Za-z]{2}$/.test(s)) return "Use a two-letter country code, like US or NL";
  return null;
}

function putSettings(values: SettingValues): Settings {
  const errors: Record<string, string> = {};
  for (const [key, value] of Object.entries(values)) {
    const f = fields[key];
    if (!f) errors[key] = "Unknown setting";
    else if (f.locked) errors[key] = f.source === "env" ? `Set by ${f.env}; change it there` : "Set in the config file; change it there";
    else if (value !== null && value !== "") {
      const e = validate(key, value);
      if (e) errors[key] = e;
    }
  }
  const effective = (k: string) => (k in values ? values[k] !== null && values[k] !== "" : fields[k]!.set && fields[k]!.source !== "default");
  if (effective("claude.oauth_token") && effective("claude.api_key")) {
    errors["claude.api_key"] = "Use either a subscription token or an API key, not both";
  }
  if (Object.keys(errors).length > 0) {
    throw new ApiError(400, "Some settings are not valid", errors);
  }
  for (const [key, value] of Object.entries(values)) {
    if (value === null || value === "") fields[key] = defaultField(key);
    else {
      const spec = SPECS[key]!;
      const typed = spec.kind === "int" || spec.kind === "float" ? Number(value) : String(value).trim();
      put(key, { value: fields[key]!.secret ? undefined : typed, source: "ui", hint: undefined });
    }
  }
  return settingsResponse();
}

function testService({ service, values = {} }: { service: Service; values?: SettingValues }): TestResult {
  const get = (k: string) => (k in values ? values[k] : fields[k]?.set ? (fields[k]!.value ?? "saved") : null);
  // tmdb and claude have no address.
  const url = service === "tmdb" || service === "claude" ? null : get(`${service}.url`);
  if (service !== "tmdb" && service !== "claude" && (!url || validate(`${service}.url`, url) !== null)) {
    return { status: "fail", detail: "Enter the address first, like http://192.168.1.10:7878" };
  }
  switch (service) {
    case "radarr":
      return { status: "ok", detail: "Radarr 6.3.0.10514", discovered_api_key: !get("radarr.api_key") };
    case "sonarr":
      return get("sonarr.api_key")
        ? { status: "ok", detail: "Sonarr 4.0.19.2979" }
        : { status: "fail", detail: "GET /api/v3/system/status: HTTP 401. Sonarr asks for a login on this network, so paste its API key." };
    case "plex":
      return get("plex.token") ? { status: "ok", detail: "Plex Media Server 1.41.8" } : { status: "fail", detail: "Plex needs a token" };
    case "jellyfin":
      return { status: "fail", detail: `GET /System/Info: dial tcp: lookup ${String(url).replace(/^https?:\/\//, "").split(/[:/]/)[0]}: no such host` };
    case "tmdb":
      return get("tmdb.api_key") ? { status: "ok", detail: "TMDB answered" } : { status: "fail", detail: "Paste a TMDB API key first" };
    case "claude":
      return get("claude.oauth_token") || get("claude.api_key")
        ? { status: "ok", detail: "Claude replied in 3.4 s (claude-haiku-4-5)" }
        : { status: "fail", detail: "No credential: paste a subscription token or an API key" };
  }
}

// ---------- routing ----------

const options: Record<"radarr" | "sonarr", AppOptions> = {
  radarr: {
    quality_profiles: [
      { id: 7, name: "Remux + WEB 2160p" }, { id: 6, name: "Remux + WEB 1080p" },
      { id: 5, name: "UHD Bluray + WEB" }, { id: 4, name: "HD Bluray + WEB" },
    ],
    root_folders: [{ id: 1, path: "/media/movies", free_space: 3.1 * 1024 ** 4 }],
    default_root_folder: "",
  },
  sonarr: {
    quality_profiles: [
      { id: 7, name: "Remux + WEB 2160p" }, { id: 6, name: "Remux + WEB 1080p" },
      { id: 4, name: "HD Bluray + WEB" }, { id: 5, name: "UHD Bluray + WEB" },
    ],
    root_folders: [
      { id: 1, path: "/media/series", free_space: 3.1 * 1024 ** 4 },
      { id: 2, path: "/media/anime", free_space: 812 * 1024 ** 3 },
    ],
    default_root_folder: "",
  },
};

function latestSucceeded(kind: Kind): Run | undefined {
  return [...runs].reverse().find((r) => r.kind === kind && r.status === "succeeded");
}

function setVerdictFor(pick: Pick, verdict: Verdict | "", laterDays?: number) {
  for (const p of picks.filter((p) => p.tmdb_id === pick.tmdb_id && p.kind === pick.kind)) {
    p.verdict = verdict || undefined;
    p.verdict_at = verdict ? new Date().toISOString() : undefined;
    p.later_until = verdict === "later" ? inDays(laterDays ?? 30) : undefined;
  }
}

export async function mockFetch(method: string, path: string, body?: unknown): Promise<unknown> {
  await wait(method === "POST" ? 450 : 220);
  if (scenario === "down") throw new ApiError(0, "Proposarr is not reachable. Check that `proposarr serve` is running.");
  const url = new URL(path, "http://mock");
  const q = url.searchParams;
  const parts = url.pathname.split("/").filter(Boolean); // ["api", ...]
  const configured = scenario !== "unconfigured" && !(scenario === "setup" && missing().length > 0);

  if (method === "GET" && url.pathname === "/api/status") {
    const m = missing();
    return {
      version: "0.2.0-dev",
      claude_auth: fields["claude.oauth_token"]!.set ? "oauth_token" : fields["claude.api_key"]!.set ? "api_key" : "local",
      connections: {
        radarr: !!fields["radarr.url"]!.set && configured,
        sonarr: !!fields["sonarr.url"]!.set && configured,
        tmdb: fields["tmdb.api_key"]!.set,
        plex: fields["plex.url"]!.set && fields["plex.token"]!.set,
        jellyfin: fields["jellyfin.url"]!.set && fields["jellyfin.api_key"]!.set,
      },
      running: runs.filter((r) => r.status === "running").map((r) => ({ run_id: r.id, kind: r.kind, message: progressFor.get(r.id) ?? "Starting…" })),
      setup_required: m.length > 0,
      missing: m,
    } satisfies Status;
  }
  if (url.pathname === "/api/settings" && method === "GET") return settingsResponse();
  if (url.pathname === "/api/settings" && method === "PUT") return putSettings((body as { values: SettingValues }).values);
  if (url.pathname === "/api/settings/test" && method === "POST") {
    await wait(1200);
    return testService(body as { service: Service; values?: SettingValues });
  }
  if (method === "GET" && url.pathname === "/api/connections/check") {
    await wait(1100);
    if (!configured) {
      return [
        { name: "Radarr", status: "skip", detail: "not configured" },
        { name: "Sonarr", status: "skip", detail: "not configured" },
        { name: "Radarr or Sonarr", status: "fail", detail: "at least one must be configured" },
        { name: "TMDB", status: "fail", detail: "not configured, PROPOSARR_TMDB_API_KEY is required" },
        { name: "Plex", status: "skip", detail: "not configured" },
        { name: "Jellyfin", status: "skip", detail: "not configured" },
        { name: "claude", status: "ok", detail: "2.1.268 (Claude Code)" },
        { name: "Claude auth", status: "ok", detail: "via local claude login" },
      ] satisfies CheckResult[];
    }
    return [
      { name: "Radarr", status: "ok", detail: "6.3.0.10514" },
      { name: "Sonarr", status: "ok", detail: "4.0.19.2979" },
      { name: "TMDB", status: "ok", detail: "" },
      { name: "Plex", status: "fail", detail: "GET /identity: dial tcp 192.168.1.238:32400: i/o timeout" },
      { name: "Jellyfin", status: "skip", detail: "not configured" },
      { name: "claude", status: "ok", detail: "2.1.268 (Claude Code)" },
      { name: "Claude auth", status: "ok", detail: "via local claude login" },
    ] satisfies CheckResult[];
  }
  if (method === "GET" && url.pathname === "/api/config") {
    return {
      listen: ":8585", data_dir: "/config/data", history_days: 180, snapshot_ttl: "6h0m0s",
      radarr: { url: configured ? "http://192.168.1.238:7878" : "", api_key_set: false, root_folder: "", minimum_availability: "released" },
      sonarr: { url: configured ? "http://192.168.1.238:8989" : "", api_key_set: false, root_folder: "" },
      plex: { url: configured ? "http://192.168.1.238:32400" : "", token_set: configured },
      jellyfin: { url: "", api_key_set: false, user_id: "" },
      tmdb: { api_key_set: configured, region: "NL" },
      claude: { bin: "claude", auth: "local", timeout: "10m0s", max_budget_usd: 0 },
      movies: { model: "claude-sonnet-5", effort: "medium", picks: 10, candidates: 150, free_picks: 3, seeds: 15, top_titles: 500 },
      series: { model: "claude-sonnet-5", effort: "medium", picks: 10, candidates: 150, free_picks: 3, seeds: 15, top_titles: 500 },
    } satisfies AppConfig;
  }
  if (method === "GET" && url.pathname === "/api/runs") {
    return [...runs].reverse().slice(0, Number(q.get("limit") ?? 50)).map((r) => ({ ...r }));
  }
  if (method === "POST" && url.pathname === "/api/runs") {
    const { kind, vibe, use_taste } = body as { kind: Kind; vibe?: string; use_taste?: boolean };
    const useTaste = use_taste !== false;
    if (!configured) throw new ApiError(400, `proposarr run --kind ${kind} needs PROPOSARR_TMDB_API_KEY (tmdb.api_key)`);
    if (!useTaste && !vibe?.trim()) throw new ApiError(400, "an open search needs a description: vibe is required when use_taste is false");    if (runs.some((r) => r.kind === kind && r.status === "running")) throw new ApiError(409, `a ${kind} run is already running`);
    return { ...startMockRun(kind, vibe?.trim() ?? "", 14_000, useTaste) };
  }
  if (parts[1] === "runs" && parts[2] && method === "GET") {
    const run = runs.find((r) => r.id === Number(parts[2]));
    if (!run) throw new ApiError(404, "run not found");
    return { run: { ...run, profile: run.status === "succeeded" && run.use_taste ? profileFor(run.kind) : undefined }, picks: picks.filter((p) => p.run_id === run.id) };
  }
  if (method === "GET" && url.pathname === "/api/picks") {
    const kind = q.get("kind") as Kind | null;
    const runParam = q.get("run");
    const verdict = q.get("verdict");
    let list = picks.filter((p) => !kind || p.kind === kind);
    if (runParam === "latest") {
      const ids = new Set((["movies", "series"] as Kind[]).map((k) => latestSucceeded(k)?.id));
      list = list.filter((p) => ids.has(p.run_id));
    } else if (runParam) {
      list = list.filter((p) => p.run_id === Number(runParam));
    }
    if (verdict === "none") list = list.filter((p) => !p.verdict);
    else if (verdict) list = list.filter((p) => p.verdict === verdict);
    return list.sort((a, b) => b.run_id - a.run_id || b.score - a.score).map((p) => ({ ...p }));
  }
  if (parts[1] === "picks" && parts[3] === "verdict" && method === "POST") {
    const pick = picks.find((p) => p.id === Number(parts[2]));
    if (!pick) throw new ApiError(404, "pick not found");
    const { verdict, later_days } = body as { verdict: Verdict | ""; later_days?: number };
    setVerdictFor(pick, verdict, later_days);
    emit({ type: "pick.updated", data: { ...pick } });
    return { ...pick };
  }
  if (parts[1] === "apps" && parts[3] === "options") {
    return options[parts[2] as "radarr" | "sonarr"];
  }
  if (parts[1] === "picks" && parts[3] === "request" && method === "POST") {
    await wait(700);
    const pick = picks.find((p) => p.id === Number(parts[2]));
    if (!pick) throw new ApiError(404, "pick not found");
    const { quality_profile_id, root_folder } = body as { quality_profile_id?: number; root_folder?: string };
    const app = pick.kind === "series" ? "sonarr" : "radarr";
    const profile = options[app].quality_profiles.find((p) => p.id === quality_profile_id);
    if (!profile) throw new ApiError(400, "quality_profile_id is required: choose a quality profile for this title");
    if (options[app].root_folders.length > 1 && !root_folder) {
      throw new ApiError(400, "several root folders exist: choose one of /media/series, /media/anime");
    }
    if (pick.tmdb_id === 1124) throw new ApiError(409, "The Prestige (2006) is already in your Radarr library");
    setVerdictFor(pick, "accepted");
    pick.request = {
      pick_id: pick.id, app, target_id: 900 + pick.id, quality_profile: profile.name,
      root_folder: root_folder ?? options[app].root_folders[0]!.path, status: "added", requested_at: new Date().toISOString(),
    };
    emit({ type: "pick.updated", data: { ...pick } });
    return { ...pick };
  }
  if (method === "GET" && url.pathname === "/api/library") {
    const kind = (q.get("kind") as Kind) ?? "movies";
    if (scenario === "empty" || !configured) return { kind, titles: [], profile: null } satisfies Library;
    return { kind, titles: kind === "series" ? librarySeries : libraryMovies, profile: profileFor(kind) } satisfies Library;
  }
  throw new ApiError(404, `mock: no route for ${method} ${path}`);
}
