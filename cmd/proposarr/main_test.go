package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leandervdo/proposarr/internal/agent"
	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/request"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolveConfigPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	flagFile := filepath.Join(dir, "flag.yaml")
	envFile := filepath.Join(dir, "env.yaml")
	for _, f := range []string{flagFile, envFile} {
		if err := os.WriteFile(f, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	missing := filepath.Join(dir, "missing.yaml")

	cases := []struct {
		name    string
		flag    string
		env     string
		local   bool
		want    string
		wantErr string
	}{
		{name: "flag wins over env", flag: flagFile, env: envFile, want: flagFile},
		{name: "missing flag file", flag: missing, wantErr: "--config"},
		{name: "env", env: envFile, want: envFile},
		{name: "missing env file", env: missing, wantErr: "PROPOSARR_CONFIG"},
		{name: "none", want: ""},
		{name: "local proposarr.yaml", local: true, want: "proposarr.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Remove("proposarr.yaml")
			if tc.local {
				if err := os.WriteFile("proposarr.yaml", []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := resolveConfigPath(tc.flag, envMap(map[string]string{"PROPOSARR_CONFIG": tc.env}))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func chooser(input string) (*terminalChooser, *bytes.Buffer) {
	out := &bytes.Buffer{}
	return newTerminalChooser(bufio.NewReader(strings.NewReader(input)), out), out
}

var (
	testItem     = request.Item{Kind: media.Movies, TMDBID: 329865, Title: "Arrival", Year: 2016}
	testProfiles = []arr.QualityProfile{{ID: 1, Name: "Any"}, {ID: 4, Name: "HD-1080p"}, {ID: 5, Name: "Ultra-HD"}}
)

func TestChooseQualityProfile(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		input   string
		wantID  int
		wantErr error
	}{
		{name: "number", input: "2\n", wantID: 4},
		{name: "name case-insensitive", input: "ultra-hd\n", wantID: 5},
		{name: "invalid then valid", input: "9\nnope\n1\n", wantID: 1},
		{name: "enter without default re-prompts", input: "\n3\n", wantID: 5},
		{name: "q stops", input: "q\n", wantErr: request.ErrCancelled},
		{name: "eof stops", input: "", wantErr: request.ErrCancelled},
		{name: "last line without newline", input: "2", wantID: 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch, out := chooser(tc.input)
			got, err := ch.ChooseQualityProfile(ctx, testItem, "Radarr", testProfiles)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != tc.wantID {
				t.Fatalf("got profile %d, want %d", got.ID, tc.wantID)
			}
			if !strings.Contains(out.String(), "Quality profile for Arrival (2016) in Radarr:") {
				t.Fatalf("missing header in %q", out.String())
			}
		})
	}
}

func TestChooseQualityProfileDefaultAfterFirst(t *testing.T) {
	ctx := context.Background()
	ch, out := chooser("2\n\n")
	first, err := ch.ChooseQualityProfile(ctx, testItem, "Radarr", testProfiles)
	if err != nil || first.ID != 4 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	second, err := ch.ChooseQualityProfile(ctx, request.Item{Kind: media.Movies, Title: "Contact", Year: 1997}, "Radarr", testProfiles)
	if err != nil || second.ID != 4 {
		t.Fatalf("second = %+v, %v", second, err)
	}
	if !strings.Contains(out.String(), "Enter = HD-1080p") {
		t.Fatalf("default not offered: %q", out.String())
	}
	// The default is per app: Sonarr has not been asked yet.
	ch.in = bufio.NewReader(strings.NewReader("\n1\n"))
	got, err := ch.ChooseQualityProfile(ctx, request.Item{Kind: media.Series, Title: "Severance"}, "Sonarr", testProfiles)
	if err != nil || got.ID != 1 {
		t.Fatalf("sonarr = %+v, %v", got, err)
	}
}

func TestChooseRootFolder(t *testing.T) {
	ch, out := chooser("/tv\n")
	folders := []arr.RootFolder{{Path: "/movies", FreeSpace: 2_500_000_000}, {Path: "/tv"}}
	got, err := ch.ChooseRootFolder(context.Background(), testItem, "Radarr", folders)
	if err != nil || got.Path != "/tv" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if !strings.Contains(out.String(), "/movies (2.5 GB free)") {
		t.Fatalf("free space missing: %q", out.String())
	}
}

func TestConfirm(t *testing.T) {
	ch, _ := chooser("y\n\nmaybe\nq\n")
	ctx := context.Background()
	for _, want := range []answer{answerYes, answerNo, answerQuit, answerQuit} {
		got, err := ch.confirm(ctx, "Add? ")
		if err != nil || got != want {
			t.Fatalf("got %v, %v; want %v", got, err, want)
		}
	}
}

func TestRenderRun(t *testing.T) {
	run := &pipeline.Run{
		Kind:           media.Movies,
		Model:          "claude-sonnet-5",
		Effort:         "medium",
		NumTurns:       3,
		CostUSD:        0.12341,
		Usage:          agent.Usage{InputTokens: 200, CacheCreationInputTokens: 45000, OutputTokens: 3100},
		CandidateCount: 150,
		Picks: []pipeline.Pick{
			{TMDBID: 329865, IMDBID: "tt2543164", Title: "Arrival", Year: 2016, Score: 92, Reason: "Because you rewatched Interstellar.",
				RelatedTo: []string{"Interstellar", "Contact"}, Streaming: []string{"Netflix"},
				Ratings: &pipeline.Ratings{IMDB: &pipeline.IMDBRating{Value: 7.9, Votes: 850000}, RottenTomatoes: 94}},
			{TMDBID: 1, Title: "Bare", Score: 50},
		},
	}
	var buf bytes.Buffer
	renderRun(&buf, run)
	out := buf.String()
	for _, want := range []string{
		"Movies · claude-sonnet-5/medium · 3 turns · $0.1234 · 45.2k in / 3.1k out · 150 candidates",
		" 1. Arrival (2016)  score 92  tmdb:329865",
		"    Because you rewatched Interstellar.",
		"    Related: Interstellar, Contact · Streaming: Netflix · IMDb 7.9 · RT 94% · IMDb: https://www.imdb.com/title/tt2543164/",
		" 2. Bare  score 50  tmdb:1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Related: \n") || strings.Count(out, "Related:") != 1 || strings.Contains(out, "Search:") {
		t.Errorf("empty details should be omitted:\n%s", out)
	}

	buf.Reset()
	renderRun(&buf, &pipeline.Run{Kind: media.Series, OpenSearch: true, Vibe: "short Korean thrillers", Model: "claude-sonnet-5", NumTurns: 1,
		Picks: []pipeline.Pick{{TMDBID: 2, Title: "Signal", Score: 85, Source: "free", RelatedTo: []string{}}}})
	out = buf.String()
	if !strings.Contains(out, "Series · open search · claude-sonnet-5 · 1 turn") || strings.Contains(out, "candidates") ||
		!strings.Contains(out, `Search: "short Korean thrillers"`) || strings.Contains(out, "Vibe:") {
		t.Errorf("open search output:\n%s", out)
	}
}

func TestRunNoTasteNeedsVibe(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, args := range [][]string{
		{"run", "--kind", "movies", "--no-taste"},
		{"run", "--kind", "series", "--no-taste", "--vibe", "   "},
	} {
		c, _, stderr := newTestCLI(false)
		if code := c.dispatch(context.Background(), args); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
		if !strings.Contains(stderr.String(), "--no-taste needs --vibe") {
			t.Errorf("%v: stderr %q", args, stderr.String())
		}
	}
	// With a description the flags are valid; the run then stops at the missing config.
	c, _, stderr := newTestCLI(false)
	if code := c.dispatch(context.Background(), []string{"run", "--kind", "movies", "--no-taste", "--vibe", "90s heist movies"}); code != 1 {
		t.Fatalf("exit %d, want 1 (stderr %q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "PROPOSARR_TMDB_API_KEY") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

type fakeLookup struct {
	ids []int
	l   *arr.Lookup
	err error
}

func (f *fakeLookup) Lookup(_ context.Context, id int) (*arr.Lookup, error) {
	f.ids = append(f.ids, id)
	return f.l, f.err
}

type fakeTVDB map[int]int

func (f fakeTVDB) TVDBID(_ context.Context, tmdbID int) (int, error) { return f[tmdbID], nil }

func TestArrRatings(t *testing.T) {
	ctx := context.Background()
	radarr := &fakeLookup{l: &arr.Lookup{Ratings: arr.Ratings{IMDB: 8.7, IMDBVotes: 2275363, RottenTomatoes: 83, Metacritic: 73}}}
	sonarr := &fakeLookup{l: &arr.Lookup{Ratings: arr.Ratings{IMDB: 9.5, IMDBVotes: 2666038}}}
	a := &arrRatings{radarr: radarr, sonarr: sonarr, tvdb: fakeTVDB{1396: 81189}}

	got, err := a.Ratings(ctx, media.Movies, 157336)
	if err != nil || got == nil || got.IMDB == nil || *got.IMDB != (pipeline.IMDBRating{Value: 8.7, Votes: 2275363}) ||
		got.RottenTomatoes != 83 || got.Metacritic != 73 || radarr.ids[0] != 157336 {
		t.Fatalf("movie ratings = %+v, %v (lookups %v)", got, err, radarr.ids)
	}
	got, err = a.Ratings(ctx, media.Series, 1396)
	if err != nil || got == nil || got.IMDB.Value != 9.5 || got.RottenTomatoes != 0 || len(sonarr.ids) != 1 || sonarr.ids[0] != 81189 {
		t.Fatalf("series ratings = %+v, %v (lookups %v)", got, err, sonarr.ids)
	}
	if got, err := a.Ratings(ctx, media.Series, 7); got != nil || err != nil || len(sonarr.ids) != 1 {
		t.Fatalf("series without tvdb id = %+v, %v (lookups %v)", got, err, sonarr.ids)
	}
	radarr.l = &arr.Lookup{}
	if got, err := a.Ratings(ctx, media.Movies, 1); got != nil || err != nil {
		t.Fatalf("unrated movie = %+v, %v", got, err)
	}
	radarr.err = errors.New("radarr: movie tmdb:1 not found")
	if _, err := a.Ratings(ctx, media.Movies, 1); err == nil {
		t.Fatal("lookup error swallowed")
	}
	if got, err := (&arrRatings{radarr: radarr}).Ratings(ctx, media.Series, 1396); got != nil || err != nil {
		t.Fatalf("no sonarr = %+v, %v", got, err)
	}
}

func TestRenderDiagnostics(t *testing.T) {
	var buf bytes.Buffer
	renderDiagnostics(&buf, &pipeline.Run{
		Warnings: []string{"plex history failed: timeout"},
		Rejected: []pipeline.Rejected{{Title: "Alien", Reason: "already in library"}, {TMDBID: 7, Reason: "unresolved"}},
	})
	for _, want := range []string{"warning: plex history failed: timeout", "rejected: Alien — already in library", "rejected: tmdb:7 — unresolved"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in %q", want, buf.String())
		}
	}
}

func newTestCLI(interactive bool) (*cli, *bytes.Buffer, *bytes.Buffer) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	return &cli{
		stdin:       strings.NewReader(""),
		stdout:      stdout,
		stderr:      stderr,
		getenv:      envMap(nil),
		interactive: interactive,
	}, stdout, stderr
}

func TestAddNeedsQualityProfileWithoutTerminal(t *testing.T) {
	// No config exists, so reaching config or network use would fail differently.
	t.Chdir(t.TempDir())
	c, _, stderr := newTestCLI(false)
	code := c.dispatch(context.Background(), []string{"add", "--kind", "movies", "--tmdb", "603"})
	if code != 2 {
		t.Fatalf("exit %d, want 2 (stderr %q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "stdin is not a terminal: pass --quality-profile") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestAddFlagValidation(t *testing.T) {
	c, _, _ := newTestCLI(false)
	if _, err := c.parseAdd([]string{"--kind", "movies", "--tmdb", "603", "--quality-profile", "HD-1080p"}); err != nil {
		t.Fatalf("scripted add rejected: %v", err)
	}
	for _, args := range [][]string{
		{"--tmdb", "603", "--quality-profile", "x"},
		{"--kind", "movies", "--quality-profile", "x"},
		{"--kind", "books", "--tmdb", "1", "--quality-profile", "x"},
	} {
		var ue usageError
		if _, err := c.parseAdd(args); !errors.As(err, &ue) {
			t.Errorf("%v: err = %v, want usage error", args, err)
		}
	}
}

func TestAddMissingAppConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	c, _, stderr := newTestCLI(false)
	code := c.dispatch(context.Background(), []string{"add", "--kind", "series", "--tmdb", "95396", "--quality-profile", "HD"})
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	for _, want := range []string{"PROPOSARR_SONARR_URL", "PROPOSARR_SONARR_API_KEY", "PROPOSARR_TMDB_API_KEY"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr missing %q: %q", want, stderr.String())
		}
	}
}

func TestRunAddGuards(t *testing.T) {
	t.Chdir(t.TempDir())
	cases := []struct {
		interactive bool
		args        []string
		want        string
	}{
		{false, []string{"run", "--kind", "movies", "--add"}, "--add needs an interactive terminal"},
		{true, []string{"run", "--kind", "movies", "--add", "--json"}, "--add cannot be combined with --json"},
		{true, []string{"run"}, "--kind is required"},
	}
	for _, tc := range cases {
		c, _, stderr := newTestCLI(tc.interactive)
		if code := c.dispatch(context.Background(), tc.args); code != 2 {
			t.Errorf("%v: exit %d, want 2", tc.args, code)
		}
		if !strings.Contains(stderr.String(), tc.want) {
			t.Errorf("%v: stderr %q missing %q", tc.args, stderr.String(), tc.want)
		}
	}
}

func TestRunMissingConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	c, _, stderr := newTestCLI(false)
	if code := c.dispatch(context.Background(), []string{"run", "--kind", "movies"}); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	for _, want := range []string{"PROPOSARR_TMDB_API_KEY", "PROPOSARR_RADARR_URL", "PROPOSARR_RADARR_API_KEY"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr missing %q: %q", want, stderr.String())
		}
	}
}

func TestDispatch(t *testing.T) {
	c, stdout, _ := newTestCLI(false)
	if code := c.dispatch(context.Background(), []string{"version"}); code != 0 || strings.TrimSpace(stdout.String()) != version {
		t.Fatalf("version: exit %d, out %q", code, stdout.String())
	}
	c, _, stderr := newTestCLI(false)
	if code := c.dispatch(context.Background(), []string{"bogus"}); code != 2 || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("bogus: exit %d, err %q", code, stderr.String())
	}
	c, _, _ = newTestCLI(false)
	if code := c.dispatch(context.Background(), []string{"run", "-h"}); code != 0 {
		t.Fatalf("run -h: exit %d", code)
	}
}
