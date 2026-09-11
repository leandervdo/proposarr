package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leandervdo/proposarr/internal/agent"
	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/candidates"
	"github.com/leandervdo/proposarr/internal/history"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/profile"
	"github.com/leandervdo/proposarr/internal/tmdb"
)

var now = time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

type fakeLib struct {
	titles []media.Title
	err    error
}

func (f fakeLib) Titles(context.Context, media.Kind) ([]media.Title, error) { return f.titles, f.err }

type fakeMeta struct {
	mu      sync.Mutex
	recs    map[int][]tmdb.Item
	details map[int]tmdb.Details
	search  map[string][]tmdb.Item // keyed by media.NormTitle(query)
}

func (m *fakeMeta) Recommendations(_ context.Context, _ media.Kind, id int) ([]tmdb.Item, error) {
	return m.recs[id], nil
}

func (m *fakeMeta) Similar(context.Context, media.Kind, int) ([]tmdb.Item, error) { return nil, nil }

func (m *fakeMeta) Details(_ context.Context, _ media.Kind, id int, _ string) (tmdb.Details, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.details[id]
	if !ok {
		return tmdb.Details{}, fmt.Errorf("tmdb: %d not found", id)
	}
	return d, nil
}

func (m *fakeMeta) Search(_ context.Context, _ media.Kind, q string, _ int) ([]tmdb.Item, error) {
	return m.search[media.NormTitle(q)], nil
}

type fakeHistory struct {
	name    string
	entries []history.Entry
	err     error
}

func (f fakeHistory) Name() string               { return f.name }
func (f fakeHistory) Ping(context.Context) error { return nil }
func (f fakeHistory) History(context.Context, media.Kind, time.Time) ([]history.Entry, error) {
	return f.entries, f.err
}

func item(id int, title string, year int) tmdb.Item {
	return tmdb.Item{ID: id, Kind: media.Movies, Title: title, Year: year, Genres: []string{"Science Fiction"}, Rating: 7.5, Votes: 1000}
}

func det(id int, title string, year int, providers ...string) tmdb.Details {
	return tmdb.Details{
		Item:      tmdb.Item{ID: id, Kind: media.Movies, Title: title, Year: year, Overview: title + " overview", Genres: []string{"Drama"}, Rating: 8, PosterPath: fmt.Sprintf("/%d.jpg", id)},
		Providers: providers,
	}
}

func pk(id int, title string, year, score int, source string, related ...string) map[string]any {
	return map[string]any{"tmdb_id": id, "title": title, "year": year, "reason": "Shares its tone.", "related_to": related, "score": score, "source": source}
}

func structured(t *testing.T, picks ...map[string]any) []byte {
	t.Helper()
	if picks == nil {
		picks = []map[string]any{}
	}
	b, err := json.Marshal(map[string]any{"picks": picks})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fixture() (fakeLib, fakeHistory, *fakeMeta) {
	lib := fakeLib{titles: []media.Title{
		{TMDBID: 157336, Title: "Interstellar", Year: 2014, Genres: []string{"Science Fiction"}},
		{TMDBID: 686, Title: "Contact", Year: 1997, Genres: []string{"Science Fiction", "Drama"}},
	}}
	hist := fakeHistory{name: "plex", entries: []history.Entry{
		{Kind: media.Movies, TMDBID: 157336, Title: "Interstellar", Year: 2014, Plays: 2, LastWatched: now.Add(-24 * time.Hour), Signal: history.Rewatched},
		{Kind: media.Movies, TMDBID: 1091, Title: "The Thing", Year: 1982, Plays: 1, LastWatched: now.Add(-48 * time.Hour), Signal: history.Watched},
	}}
	thing := det(1091, "The Thing", 1982)
	thing.Genres = []string{"Horror"}
	meta := &fakeMeta{
		recs: map[int][]tmdb.Item{
			157336: {item(329865, "Arrival", 2016), item(49047, "Gravity", 2013), item(686, "Contact", 1997)},
			1091:   {item(348, "Alien", 1979), item(329865, "Arrival", 2016)},
		},
		details: map[int]tmdb.Details{
			1091:   thing,
			329865: det(329865, "Arrival", 2016, "Netflix"),
			49047:  det(49047, "Gravity", 2013),
			348:    det(348, "Alien", 1979),
			603:    det(603, "The Matrix", 1999, "Max"),
			686:    det(686, "Contact", 1997),
		},
		search: map[string][]tmdb.Item{
			"matrix": {item(603, "The Matrix", 1999)},
		},
	}
	return lib, hist, meta
}

func TestRunHappyPath(t *testing.T) {
	lib, hist, meta := fixture()
	var dir string
	fake := &agent.Fake{Fn: func(o agent.Options) (agent.Result, error) {
		dir = o.Dir
		if _, err := os.Stat(o.Dir); err != nil {
			t.Errorf("run dir missing during agent run: %v", err)
		}
		return agent.Result{
			Structured: structured(t,
				pk(329865, "Arrival", 2016, 91, "candidate", "Interstellar (2014)", "Nope"),
				pk(329865, "Arrival", 2016, 80, "candidate"),
				pk(0, "The Matrix", 1999, 120, "free", "the thing"),
				pk(0, "Blade Runner", 1982, 88, "free"),
				pk(49047, "Gravity", 2013, 70, "candidate"),
				pk(348, "Alien", 1979, 60, "candidate"),
			),
			CostUSD: 0.12, NumTurns: 3, SessionID: "s1",
			Usage: agent.Usage{InputTokens: 100, OutputTokens: 50},
		}, nil
	}}
	var progress []string
	p := New(Deps{Library: lib, History: []history.Source{hist}, Meta: meta, Agent: fake,
		Progress: func(m string) { progress = append(progress, m) }, Now: func() time.Time { return now }})

	req := Request{Kind: media.Movies, Vibe: "slow cerebral sci-fi", Model: "claude-sonnet-5", Effort: "medium",
		Picks: 3, FreePicks: 1, AgentEnv: []string{"CLAUDE_CODE_OAUTH_TOKEN=x"}}
	run, err := p.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if run.LibraryCount != 2 || run.HistoryCount != 2 || run.CandidateCount != 3 {
		t.Errorf("counts lib=%d hist=%d cand=%d", run.LibraryCount, run.HistoryCount, run.CandidateCount)
	}
	if run.CostUSD != 0.12 || run.NumTurns != 3 || run.SessionID != "s1" || run.Usage.InputTokens != 100 {
		t.Errorf("agent result not copied: %+v", run)
	}
	if run.FinishedAt.IsZero() || len(progress) == 0 {
		t.Errorf("finished=%v progress=%v", run.FinishedAt, progress)
	}

	want := []struct {
		id      int
		score   int
		source  string
		stream  []string
		related []string
	}{
		{603, 100, "free", []string{"Max"}, []string{"The Thing (1982)"}},
		{329865, 91, "candidate", []string{"Netflix"}, []string{"Interstellar (2014)"}},
		{49047, 70, "candidate", nil, []string{}},
	}
	if len(run.Picks) != len(want) {
		t.Fatalf("picks = %+v", run.Picks)
	}
	for i, w := range want {
		got := run.Picks[i]
		if got.TMDBID != w.id || got.Score != w.score || got.Source != w.source || got.Kind != media.Movies {
			t.Errorf("pick %d = %+v, want %+v", i, got, w)
		}
		if strings.Join(got.Streaming, ",") != strings.Join(w.stream, ",") || strings.Join(got.RelatedTo, ",") != strings.Join(w.related, ",") {
			t.Errorf("pick %d streaming=%v related=%v", i, got.Streaming, got.RelatedTo)
		}
		if got.RelatedTo == nil {
			t.Errorf("pick %d related_to must be non-nil", i)
		}
	}
	if run.Picks[1].PosterURL != "https://image.tmdb.org/t/p/w500/329865.jpg" || run.Picks[1].Overview != "Arrival overview" {
		t.Errorf("arrival not enriched: %+v", run.Picks[1])
	}

	reasons := map[string]string{}
	for _, r := range run.Rejected {
		reasons[r.Title] = r.Reason
	}
	if reasons["Arrival"] != "duplicate" || reasons["Blade Runner"] != "free pick limit reached" || reasons["Alien"] != "over pick count" || len(run.Rejected) != 3 {
		t.Errorf("rejected = %+v", run.Rejected)
	}

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("agent calls = %d", len(calls))
	}
	o := calls[0]
	if o.Model != "claude-sonnet-5" || o.Effort != "medium" || len(o.Env) != 1 || o.Env[0] != "CLAUDE_CODE_OAUTH_TOKEN=x" {
		t.Errorf("options = %+v", o)
	}
	if !strings.Contains(o.Prompt, "slow cerebral sci-fi") || !strings.Contains(o.Prompt, "329865 | Arrival (2016)") {
		t.Errorf("prompt missing vibe or candidates:\n%s", o.Prompt)
	}
	if o.SystemPrompt == "" || strings.Contains(o.SystemPrompt, "Arrival") || strings.Contains(o.JSONSchema, "Arrival") {
		t.Error("candidate data leaked outside Prompt")
	}
	if o.MCPConfigPath != "" || len(o.AllowedTools) != 0 || !strings.Contains(o.JSONSchema, `"maxItems":3`) {
		t.Errorf("options = %+v", o)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("run dir %q not removed: %v", dir, err)
	}
}

func TestRunRejects(t *testing.T) {
	lib, hist, meta := fixture()
	hist.entries = append(hist.entries, history.Entry{Kind: media.Movies, Title: "Primer", Year: 2004, Plays: 1, LastWatched: now, Signal: history.Watched})
	meta.search["made up film"] = nil
	meta.search["primer"] = []tmdb.Item{item(14337, "Primer", 2004)}
	meta.search["solaris"] = []tmdb.Item{item(593, "Solaris", 1972)}
	delete(meta.details, 49047)

	fake := &agent.Fake{Result: agent.Result{Structured: structured(t,
		pk(686, "Contact", 1997, 90, "free"),
		pk(0, "Made Up Film", 2020, 90, "free"),
		pk(0, "Primer", 2004, 90, "free"),
		pk(0, "Solaris", 1972, 90, "free"),
		pk(49047, "Gravity", 2013, 80, "candidate"),
	)}}
	p := New(Deps{Library: lib, History: []history.Source{hist}, Meta: meta, Agent: fake, Now: func() time.Time { return now }})
	run, err := p.Run(context.Background(), Request{Kind: media.Movies, Picks: 5, FreePicks: 2})
	if err != nil {
		t.Fatal(err)
	}

	reasons := map[string]string{}
	for _, r := range run.Rejected {
		reasons[r.Title] = r.Reason
	}
	wantReasons := map[string]string{
		"Contact":      "already in library or watch history",
		"Made Up Film": "could not resolve on TMDB",
		"Primer":       "already in library or watch history",
		"Solaris":      "id does not resolve on TMDB",
	}
	for title, reason := range wantReasons {
		if reasons[title] != reason {
			t.Errorf("%s rejected with %q, want %q (all: %+v)", title, reasons[title], reason, run.Rejected)
		}
	}
	if len(run.Picks) != 1 || run.Picks[0].TMDBID != 49047 || run.Picks[0].Overview != "" {
		t.Errorf("picks = %+v", run.Picks)
	}
	if !hasWarning(run, "tmdb details failed for 1 of 2 picks") {
		t.Errorf("warnings = %v", run.Warnings)
	}
}

func TestRunFreePickTrustsMatchingID(t *testing.T) {
	lib, hist, meta := fixture()
	meta.search = map[string][]tmdb.Item{}
	fake := &agent.Fake{Result: agent.Result{Structured: structured(t,
		pk(603, "The Matrix", 1999, 95, "free"),
		pk(348, "Alien", 1979, 80, "free"), // in the candidate list, so not a free pick
	)}}
	p := New(Deps{Library: lib, History: []history.Source{hist}, Meta: meta, Agent: fake})
	run, err := p.Run(context.Background(), Request{Kind: media.Movies, Picks: 5, FreePicks: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Picks) != 2 || run.Picks[0].Source != "free" || run.Picks[1].Source != "candidate" {
		t.Errorf("picks = %+v, rejected = %+v", run.Picks, run.Rejected)
	}
}

func TestRunHistoryFailureIsWarning(t *testing.T) {
	lib, _, meta := fixture()
	fake := &agent.Fake{Result: agent.Result{Structured: structured(t)}}
	p := New(Deps{Library: lib, Meta: meta, Agent: fake, History: []history.Source{
		fakeHistory{name: "plex", err: errors.New("boom")},
		fakeHistory{name: "jellyfin", entries: []history.Entry{{Kind: media.Movies, TMDBID: 157336, Title: "Interstellar", Year: 2014, Plays: 1, LastWatched: now, Signal: history.Watched}}},
	}})
	run, err := p.Run(context.Background(), Request{Kind: media.Movies})
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(run, "plex history failed: boom") || run.HistoryCount != 1 {
		t.Errorf("warnings=%v history=%d", run.Warnings, run.HistoryCount)
	}
}

func TestRunSessionLimitReturnsPartialRun(t *testing.T) {
	lib, hist, meta := fixture()
	fake := &agent.Fake{Result: agent.Result{CostUSD: 0.05, SessionID: "s2"}, Err: &agent.SessionLimitError{Message: "resets 5pm"}}
	p := New(Deps{Library: lib, History: []history.Source{hist}, Meta: meta, Agent: fake})
	run, err := p.Run(context.Background(), Request{Kind: media.Movies})
	var sl *agent.SessionLimitError
	if !errors.As(err, &sl) {
		t.Fatalf("err = %v, want SessionLimitError", err)
	}
	if run == nil || run.CandidateCount != 3 || run.CostUSD != 0.05 || run.SessionID != "s2" || run.FinishedAt.IsZero() {
		t.Errorf("partial run = %+v", run)
	}
}

func TestRunNoCandidates(t *testing.T) {
	fake := &agent.Fake{}
	p := New(Deps{Library: fakeLib{}, Meta: &fakeMeta{}, Agent: fake})
	run, err := p.Run(context.Background(), Request{Kind: media.Movies})
	if err == nil || !strings.Contains(err.Error(), "no candidates") || run == nil {
		t.Fatalf("run=%v err=%v", run, err)
	}
	if len(fake.Calls()) != 0 {
		t.Error("agent must not run without candidates")
	}
}

func TestRunLibraryError(t *testing.T) {
	p := New(Deps{Library: fakeLib{err: errors.New("Radarr is not configured")}, Meta: &fakeMeta{}, Agent: &agent.Fake{}})
	run, err := p.Run(context.Background(), Request{Kind: media.Movies})
	if err == nil || run != nil {
		t.Fatalf("run=%v err=%v", run, err)
	}
}

func TestRunTypedNilDiscover(t *testing.T) {
	lib, hist, meta := fixture()
	var radarr *arr.Radarr
	fake := &agent.Fake{Result: agent.Result{Structured: structured(t)}}
	p := New(Deps{Library: lib, History: []history.Source{hist}, Meta: meta, Agent: fake, Discover: radarr})
	if _, err := p.Run(context.Background(), Request{Kind: media.Movies}); err != nil {
		t.Fatal(err)
	}
}

func TestUserPrompt(t *testing.T) {
	prof := profile.Profile{
		Top:    []profile.Entry{{TMDBID: 157336, Title: "Interstellar", Year: 2014, Weight: 4, Signal: "rewatched"}, {Title: "Contact", Year: 1997, Weight: 0.5, Signal: profile.SignalOwned}},
		Genres: []profile.GenreShare{{Name: "Drama", Share: 0.22}},
	}
	cands := []candidates.Candidate{{TMDBID: 329865, Title: "Arrival", Year: 2016, Genres: []string{"Science Fiction"}, Rating: 7.6, Votes: 18234,
		Sources: []string{"Interstellar (2014)"}, Overview: strings.Repeat("word ", 100)}}

	movies := userPrompt(Request{Kind: media.Movies, Picks: 10, Vibe: "cosy"}, prof, cands)
	for _, s := range []string{
		`Try to match this specific vibe/mood: "cosy".`,
		"stood the test of time",
		"329865 | Arrival (2016) | Science Fiction | 7.6 (18,234 votes) | Interstellar (2014)",
		"- Interstellar (2014) — weight 4, rewatched",
		"- Contact (1997) — weight 0.5, owned but unwatched",
		"Genre mix: Drama 22%",
		"Only recommend titles from the candidate list.",
	} {
		if !strings.Contains(movies, s) {
			t.Errorf("movie prompt missing %q", s)
		}
	}
	for _, line := range strings.Split(movies, "\n") {
		if strings.HasPrefix(line, "    ") && len([]rune(strings.TrimSpace(line))) > overviewRunes {
			t.Errorf("overview not truncated: %d runes", len([]rune(line)))
		}
	}

	series := userPrompt(Request{Kind: media.Series, Picks: 5, FreePicks: 2}, prof, cands)
	if !strings.Contains(series, "not canceled after 1-2 seasons") || !strings.Contains(series, "at most 2 titles") || strings.Contains(series, "vibe/mood") {
		t.Errorf("series prompt:\n%s", series)
	}
	if !strings.Contains(systemPrompt(media.Series), "TV show recommendation assistant") {
		t.Error("series system prompt")
	}
}

func TestPickSchema(t *testing.T) {
	s, err := pickSchema(7)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Properties struct {
			Picks struct {
				MaxItems int `json:"maxItems"`
				Items    struct {
					Required []string `json:"required"`
				} `json:"items"`
			} `json:"picks"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	if v.Properties.Picks.MaxItems != 7 || len(v.Properties.Picks.Items.Required) != 7 {
		t.Errorf("schema = %s", s)
	}
}

func TestClampScore(t *testing.T) {
	for in, want := range map[float64]int{-5: 0, 42.4: 42, 99.6: 100, 250: 100} {
		if got := clampScore(in); got != want {
			t.Errorf("clampScore(%v) = %d, want %d", in, got, want)
		}
	}
}

func hasWarning(run *Run, substr string) bool {
	for _, w := range run.Warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}
