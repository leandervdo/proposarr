package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"

	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/request"
)

type addFlags struct {
	kind           media.Kind
	tmdbID         int
	qualityProfile string
	rootFolder     string
	ifNothingFits  request.Fallback
	configPath     string
}

// parseAdd validates flags before any config or network use.
func (c *cli) parseAdd(args []string) (addFlags, error) {
	fs, cfgPath := c.newFlags("add")
	kindFlag := fs.String("kind", "", "movies or series (required)")
	id := fs.Int("tmdb", 0, "TMDB id of the title (required)")
	qp := fs.String("quality-profile", "", "quality profile name or id; asked interactively when omitted")
	root := fs.String("root-folder", "", "root folder path (default from config, or asked when there are several)")
	fallback := fs.String("if-nothing-fits", "", "movies with --quality-profile: switch to the best quality profile that finds a release, or wait (default wait); asked interactively otherwise")
	if err := parseFlags(fs, args); err != nil {
		return addFlags{}, err
	}
	kind, err := parseKind(*kindFlag)
	if err != nil {
		return addFlags{}, err
	}
	if *id <= 0 {
		return addFlags{}, usageError{"--tmdb is required (a positive TMDB id)"}
	}
	if *qp == "" && !c.interactive {
		return addFlags{}, usageError{"stdin is not a terminal: pass --quality-profile NAME|ID"}
	}
	ifNothingFits, err := request.ParseFallback(*fallback)
	if err != nil {
		return addFlags{}, usageError{"--if-nothing-fits must be switch or wait"}
	}
	return addFlags{kind: kind, tmdbID: *id, qualityProfile: *qp, rootFolder: *root, ifNothingFits: ifNothingFits, configPath: *cfgPath}, nil
}

func (c *cli) addCmd(ctx context.Context, args []string) error {
	f, err := c.parseAdd(args)
	if err != nil {
		return err
	}
	cfg, err := c.loadConfig(f.configPath)
	if err != nil {
		return err
	}
	c.resolveAPIKeys(ctx, &cfg, f.kind)
	missing := requireApp(cfg, f.kind)
	if f.kind == media.Series {
		missing = append(missing, requireTMDB(cfg)...) // Sonarr adds by TVDB id, resolved through TMDB
	}
	if err := missingError("proposarr add --kind "+string(f.kind), missing); err != nil {
		return err
	}

	d := buildDeps(cfg)
	item := request.Item{Kind: f.kind, TMDBID: f.tmdbID}
	if d.tmdb != nil {
		if det, err := d.tmdb.Details(ctx, f.kind, f.tmdbID, cfg.TMDB.Region); err == nil {
			item.Title, item.Year = det.Title, det.Year
		} else {
			fmt.Fprintf(c.stderr, "warning: TMDB details for tmdb:%d: %v\n", f.tmdbID, err)
		}
	}
	if item.Title == "" {
		item.Title = fmt.Sprintf("tmdb:%d", f.tmdbID)
	}

	req := d.requester()
	if f.rootFolder != "" {
		req.RadarrRootFolder = f.rootFolder
		req.SonarrRootFolder = f.rootFolder
	}
	var ch request.Chooser
	if f.qualityProfile != "" {
		ch = request.FixedChooser{QualityProfile: f.qualityProfile, RootFolder: f.rootFolder, Fallback: f.ifNothingFits}
	} else {
		ch = newTerminalChooser(bufio.NewReader(c.stdin), c.stderr)
	}

	res, err := req.Add(ctx, item, ch)
	switch {
	case err == nil:
		printAdded(c.stdout, res)
		followRelease(ctx, c.stdout, c.stderr, res)
		return nil
	case errors.Is(err, request.ErrAlreadyInLibrary):
		fmt.Fprintf(c.stdout, "%s is already in %s.\n", item.Label(), f.kind.App())
		return nil
	case errors.Is(err, request.ErrCancelled):
		fmt.Fprintln(c.stderr, "Cancelled, nothing added.")
		return nil
	}
	return err
}
