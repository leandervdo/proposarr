package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/leandervdo/proposarr/internal/agent"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/request"
)

func (c *cli) runCmd(ctx context.Context, args []string) error {
	fs, cfgPath := c.newFlags("run")
	kindFlag := fs.String("kind", "", "movies or series (required)")
	vibe := fs.String("vibe", "", "free-text mood for this run")
	noTaste := fs.Bool("no-taste", false, "open search: find titles matching --vibe, not based on your library or watch history")
	picks := fs.Int("picks", 0, "number of picks (default from config)")
	model := fs.String("model", "", "Claude model (default from config)")
	effort := fs.String("effort", "", "effort: low, medium, high, xhigh, max (default from config)")
	asJSON := fs.Bool("json", false, "print the run as JSON")
	refresh := fs.Bool("refresh", false, "ignore the cached library snapshot")
	add := fs.Bool("add", false, "ask to add each pick, choosing a quality profile per title")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	kind, err := parseKind(*kindFlag)
	if err != nil {
		return err
	}
	if *picks < 0 {
		return usageError{"--picks must be positive"}
	}
	if *noTaste && strings.TrimSpace(*vibe) == "" {
		return usageError{"--no-taste needs --vibe describing what you are looking for"}
	}
	if *add && *asJSON {
		return usageError{"--add cannot be combined with --json"}
	}
	if *add && !c.interactive {
		return usageError{"--add needs an interactive terminal; use `proposarr add` with --quality-profile for scripts"}
	}

	cfg, err := c.loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	c.resolveAPIKeys(ctx, &cfg, kind)
	if err := missingError("proposarr run --kind "+string(kind), append(requireTMDB(cfg), requireApp(cfg, kind)...)); err != nil {
		return err
	}
	d := buildDeps(cfg)
	env, _, err := d.agentEnv()
	if err != nil {
		return err
	}

	req := runRequest(cfg, kind, strings.TrimSpace(*vibe), *noTaste, env)
	if *picks > 0 {
		req.Picks = *picks
	}
	if *model != "" {
		req.Model = *model
	}
	if *effort != "" {
		req.Effort = *effort
	}

	p := d.pipeline(*refresh, func(msg string) { fmt.Fprintln(c.stderr, msg) }, nil)
	run, runErr := p.Run(ctx, req)
	if run != nil {
		renderDiagnostics(c.stderr, run)
	}
	if runErr != nil {
		if run != nil {
			fmt.Fprintf(c.stderr, "partial run: %d library titles, %d history entries, %d candidates\n",
				run.LibraryCount, run.HistoryCount, run.CandidateCount)
		}
		var sl *agent.SessionLimitError
		if errors.As(runErr, &sl) {
			return fmt.Errorf("the Claude subscription session limit was hit; try again after the usage window resets (%s)", sl.Message)
		}
		return runErr
	}

	if *asJSON {
		enc := json.NewEncoder(c.stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(run)
	}
	renderRun(c.stdout, run)
	if *add {
		return c.addPicks(ctx, d.requester(), run)
	}
	return nil
}

// addPicks asks per pick whether to add it; each accepted pick gets its own
// quality profile question.
func (c *cli) addPicks(ctx context.Context, req *request.Requester, run *pipeline.Run) error {
	if len(run.Picks) == 0 {
		return nil
	}
	ch := newTerminalChooser(bufio.NewReader(c.stdin), c.stderr)
	app := run.Kind.App()
	fmt.Fprintln(c.stderr)
	for _, p := range run.Picks {
		item := request.Item{Kind: run.Kind, TMDBID: p.TMDBID, Title: p.Title, Year: p.Year}
		ans, err := ch.confirm(ctx, fmt.Sprintf("Add %s to %s? [y/N/q] ", item.Label(), app))
		if err != nil {
			return err
		}
		if ans == answerQuit {
			return nil
		}
		if ans == answerNo {
			continue
		}
		res, err := req.Add(ctx, item, ch)
		switch {
		case err == nil:
			printAdded(c.stdout, res)
		case errors.Is(err, request.ErrAlreadyInLibrary):
			fmt.Fprintf(c.stderr, "%s is already in %s.\n", item.Label(), app)
		case errors.Is(err, request.ErrCancelled):
			fmt.Fprintln(c.stderr, "Stopped adding.")
			return nil
		case ctx.Err() != nil:
			return ctx.Err()
		default:
			fmt.Fprintf(c.stderr, "could not add %s: %v\n", item.Label(), err)
		}
	}
	return nil
}

func printAdded(w io.Writer, r request.Result) {
	fmt.Fprintf(w, "Added %s to %s with quality profile %s in %s\n", r.Title, r.App, r.QualityProfile, r.RootFolder)
}

func renderRun(w io.Writer, r *pipeline.Run) {
	name := "Movies"
	if r.Kind == media.Series {
		name = "Series"
	}
	parts := []string{name}
	if r.OpenSearch {
		parts = append(parts, "open search")
	}
	if r.Model != "" {
		m := r.Model
		if r.Effort != "" {
			m += "/" + r.Effort
		}
		parts = append(parts, m)
	}
	turns := "turns"
	if r.NumTurns == 1 {
		turns = "turn"
	}
	in := r.Usage.InputTokens + r.Usage.CacheCreationInputTokens + r.Usage.CacheReadInputTokens
	parts = append(parts,
		fmt.Sprintf("%d %s", r.NumTurns, turns),
		fmt.Sprintf("$%.4f", r.CostUSD),
		fmt.Sprintf("%s in / %s out", tokens(in), tokens(r.Usage.OutputTokens)),
	)
	if !r.OpenSearch {
		parts = append(parts, fmt.Sprintf("%d candidates", r.CandidateCount))
	}
	fmt.Fprintln(w, strings.Join(parts, " · "))
	switch {
	case r.OpenSearch:
		fmt.Fprintf(w, "Search: %q\n", r.Vibe)
	case r.Vibe != "":
		fmt.Fprintf(w, "Vibe: %q\n", r.Vibe)
	}
	if len(r.Picks) == 0 {
		fmt.Fprintln(w, "\nNo picks survived verification.")
		return
	}
	for i, p := range r.Picks {
		fmt.Fprintf(w, "\n%2d. %s  score %d  tmdb:%d\n", i+1, media.Label(p.Title, p.Year), p.Score, p.TMDBID)
		if p.Reason != "" {
			fmt.Fprintf(w, "    %s\n", p.Reason)
		}
		var details []string
		if len(p.RelatedTo) > 0 {
			details = append(details, "Related: "+strings.Join(p.RelatedTo, ", "))
		}
		if len(p.Streaming) > 0 {
			details = append(details, "Streaming: "+strings.Join(p.Streaming, ", "))
		}
		if r := p.Ratings; r != nil {
			if r.IMDB != nil {
				details = append(details, fmt.Sprintf("IMDb %.1f", r.IMDB.Value))
			}
			if r.RottenTomatoes > 0 {
				details = append(details, fmt.Sprintf("RT %d%%", r.RottenTomatoes))
			}
			if r.Metacritic > 0 {
				details = append(details, fmt.Sprintf("Metacritic %d", r.Metacritic))
			}
		}
		if p.IMDBID != "" {
			details = append(details, "IMDb: https://www.imdb.com/title/"+p.IMDBID+"/")
		}
		if len(details) > 0 {
			fmt.Fprintf(w, "    %s\n", strings.Join(details, " · "))
		}
	}
}

func renderDiagnostics(w io.Writer, r *pipeline.Run) {
	for _, msg := range r.Warnings {
		fmt.Fprintf(w, "warning: %s\n", msg)
	}
	for _, rej := range r.Rejected {
		label := rej.Title
		if label == "" {
			label = fmt.Sprintf("tmdb:%d", rej.TMDBID)
		}
		fmt.Fprintf(w, "rejected: %s — %s\n", label, rej.Reason)
	}
}

func tokens(n int) string {
	if n < 1000 {
		return fmt.Sprint(n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}
