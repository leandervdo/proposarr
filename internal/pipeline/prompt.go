package pipeline

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/leandervdo/proposarr/internal/candidates"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/profile"
)

const overviewRunes = 180

func singular(k media.Kind) string {
	if k == media.Series {
		return "TV show"
	}
	return "movie"
}

// systemPrompt is Recommendarr's system message, rewritten for structured output.
func systemPrompt(kind media.Kind) string {
	noun := kind.Noun()
	return fmt.Sprintf(`You are a %s recommendation assistant. You recommend titles the user does not own yet, based on their watch history and library. You MUST adhere to these CRITICAL rules:

1. NEVER recommend %s that are in the user's library, watch history or any exclusion list provided
2. Only recommend %s that truly match the user's taste profile
3. VERIFY each recommendation against these rules before returning it
4. Return only the structured output, no extra text`, singular(kind), noun, noun)
}

func userPrompt(req Request, prof profile.Profile, cands []candidates.Candidate) string {
	noun := req.Kind.Noun()
	var b strings.Builder

	fmt.Fprintf(&b, "Based on my watch history and library, recommend %d new %s I might enjoy that are CRITICALLY ACCLAIMED and HIGHLY RATED. Be brief and direct.", req.Picks, noun)
	if v := strings.TrimSpace(req.Vibe); v != "" {
		fmt.Fprintf(&b, " Try to match this specific vibe/mood: %q.", v)
	}
	fmt.Fprintf(&b, `

Prioritize %s that match these criteria:
1. Highest overall quality and critical acclaim
2. Strong thematic or stylistic connections to my current library
3. Diverse in content (not just the most obvious recommendations)
4. Include a mix of both popular and lesser-known hidden gems
5. %s
`, noun, fifthCriterion(req.Kind))

	b.WriteString("\n## Taste profile\n")
	b.WriteString("Weights: rewatched 4, watched 3, partially watched 1, owned but unwatched 0.5.\n")
	if len(prof.Genres) > 0 {
		parts := make([]string, len(prof.Genres))
		for i, g := range prof.Genres {
			parts[i] = fmt.Sprintf("%s %.0f%%", g.Name, g.Share*100)
		}
		fmt.Fprintf(&b, "Genre mix: %s\n", strings.Join(parts, ", "))
	}
	b.WriteString("Top titles:\n")
	if len(prof.Top) == 0 {
		b.WriteString("(none)\n")
	}
	for _, e := range prof.Top {
		fmt.Fprintf(&b, "- %s — weight %s, %s\n", e.Label(), strconv.FormatFloat(e.Weight, 'f', -1, 64), signalLabel(e.Signal))
	}

	b.WriteString("\n## Candidates\n")
	b.WriteString("Choose from this list. One per line: tmdb_id | title (year) | genres | TMDB rating (votes) | suggested by\n")
	if len(cands) == 0 {
		b.WriteString("(no candidates)\n")
	}
	for _, c := range cands {
		fmt.Fprintf(&b, "%d | %s | %s | %s | %s\n", c.TMDBID, media.Label(c.Title, c.Year), orDash(strings.Join(c.Genres, ", ")), rating(c.Rating, c.Votes), orDash(strings.Join(c.Sources, ", ")))
		if o := oneLine(c.Overview); o != "" {
			fmt.Fprintf(&b, "    %s\n", truncate(o, overviewRunes))
		}
	}

	b.WriteString("\n## Free picks\n")
	if req.FreePicks > 0 {
		fmt.Fprintf(&b, "You may include at most %d titles that are not in the candidate list when they are a clearly better fit. Mark them source \"free\"; give the TMDB id if you are certain, otherwise 0 — they are resolved by title and year.\n", req.FreePicks)
	} else {
		b.WriteString("Only recommend titles from the candidate list.\n")
	}

	fmt.Fprintf(&b, `
## Rules
- You MUST NOT recommend any title from the taste profile list above.
- Candidate picks use source "candidate" and the tmdb_id from the candidate list.
- related_to must name 1-3 titles exactly as written in the taste profile list.
- reason is one sentence naming what the %s shares with those titles.
- score is 0-100. Silently calculate it by privately considering ratings from IMDb, Rotten Tomatoes, Metacritic and audience ratings. Do not cite any rating source in the reason.
`, singular(req.Kind))
	return b.String()
}

func fifthCriterion(k media.Kind) string {
	if k == media.Series {
		return "Focus on complete or ongoing shows with consistent quality, not canceled after 1-2 seasons"
	}
	return "Consider both classic and recent releases that have stood the test of time"
}

const maxSearchSuggestions = 50

// searchCount is how many suggestions an open search asks for, so that rejects
// and rating-based ranking still leave enough picks.
func searchCount(picks int) int { return min(picks*2, maxSearchSuggestions) }

func searchSystemPrompt(kind media.Kind) string {
	noun := kind.Noun()
	return fmt.Sprintf(`You are a %s search assistant. You find titles that match the user's description, leaving out titles they already have. You MUST adhere to these CRITICAL rules:

1. NEVER recommend %s that are in the user's library or any exclusion list provided
2. Only recommend %s that truly match the user's request, including every explicit constraint such as decade, country, language or genre
3. VERIFY each recommendation against these rules before returning it
4. Return only the structured output, no extra text`, singular(kind), noun, noun)
}

// searchPrompt asks for n titles matching req.Vibe, listing owned titles
// (most recently added first, at most req.TopTitles) so they are avoided.
func searchPrompt(req Request, lib []media.Title, n int) string {
	noun := req.Kind.Noun()
	var b strings.Builder
	fmt.Fprintf(&b, "Recommend %d %s that match this request: %q. Be brief and direct.", n, noun, strings.TrimSpace(req.Vibe))
	fmt.Fprintf(&b, `

Prioritize %s that match these criteria:
1. Satisfies every explicit constraint in the request (decade or years, country, language, genre, length). These are hard requirements: never stretch them to fill the list
2. Highest overall quality and critical acclaim
3. Diverse in content (not just the most obvious recommendations)
4. A mix of popular titles and lesser-known hidden gems
5. %s
`, noun, fifthCriterion(req.Kind))

	b.WriteString("\n## Titles I already have — do not suggest these\n")
	owned := append([]media.Title(nil), lib...)
	sort.SliceStable(owned, func(i, j int) bool { return owned[i].Added.After(owned[j].Added) })
	if len(owned) == 0 {
		b.WriteString("(none)\n")
	}
	for i, t := range owned {
		if i == req.TopTitles {
			fmt.Fprintf(&b, "(and %d more)\n", len(owned)-i)
			break
		}
		fmt.Fprintf(&b, "- %s\n", t.Label())
	}

	one := singular(req.Kind)
	fmt.Fprintf(&b, `
## Rules
- You MUST NOT recommend any title from the list above.
- Focus on how closely each %s matches the request and on its quality. The final order comes from real IMDb and Rotten Tomatoes ratings.
- Use source "free". Give the TMDB id only if you are certain, otherwise 0 — every title is resolved by title and year.
- related_to is an empty array.
- reason is one sentence on how the %s matches the request.
- score is 0-100 and means how well the %s fits the request, not how good it is: 90 or more fits every part of it, below 70 misses a requirement (for example the wrong decade). Return fewer titles rather than titles you would score below 70.
`, one, one, one)
	return b.String()
}

func signalLabel(s string) string {
	switch s {
	case "partial":
		return "partially watched"
	case profile.SignalOwned:
		return "owned but unwatched"
	}
	return s
}

func rating(r float64, votes int) string {
	if r == 0 && votes == 0 {
		return "unrated"
	}
	return fmt.Sprintf("%.1f (%s votes)", r, thousands(votes))
}

func thousands(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func pickSchema(maxPicks int) (string, error) {
	str := map[string]any{"type": "string"}
	integer := map[string]any{"type": "integer"}
	item := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"tmdb_id", "title", "year", "reason", "related_to", "score", "source"},
		"properties": map[string]any{
			"tmdb_id":    integer,
			"title":      str,
			"year":       integer,
			"reason":     str,
			"related_to": map[string]any{"type": "array", "items": str},
			"score":      map[string]any{"type": "integer", "minimum": 0, "maximum": 100},
			"source":     map[string]any{"type": "string", "enum": []string{"candidate", "free"}},
		},
	}
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"picks"},
		"properties": map[string]any{
			"picks": map[string]any{"type": "array", "maxItems": maxPicks, "items": item},
		},
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return "", fmt.Errorf("build pick schema: %w", err)
	}
	return string(b), nil
}
