// Command proposarr proposes new movies and series for Radarr and Sonarr.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/leandervdo/proposarr/internal/config"
	"github.com/leandervdo/proposarr/internal/media"
)

var version = "dev"

// cli carries the process IO so commands can be tested without a terminal.
type cli struct {
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	getenv      func(string) string
	interactive bool // stdin is a terminal
}

// usageError exits 2. An empty message means flag parsing already printed it.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// errFailed exits 1 without printing anything more.
var errFailed = errors.New("failed")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		// A second Ctrl+C kills the process even while a prompt is blocked.
		<-ctx.Done()
		stop()
	}()
	c := &cli{
		stdin:       os.Stdin,
		stdout:      os.Stdout,
		stderr:      os.Stderr,
		getenv:      os.Getenv,
		interactive: stdinIsTerminal(),
	}
	code := c.dispatch(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}

func (c *cli) dispatch(ctx context.Context, args []string) int {
	if len(args) == 0 {
		c.usage(c.stderr)
		return 2
	}
	var err error
	switch args[0] {
	case "run":
		err = c.runCmd(ctx, args[1:])
	case "add":
		err = c.addCmd(ctx, args[1:])
	case "serve":
		err = c.serveCmd(ctx, args[1:])
	case "check":
		err = c.checkCmd(ctx, args[1:])
	case "validate-token":
		err = c.tokenCmd(ctx, args[1:])
	case "version", "--version":
		fmt.Fprintln(c.stdout, version)
	case "help", "-h", "--help":
		c.usage(c.stdout)
	default:
		fmt.Fprintf(c.stderr, "unknown command %q\n\n", args[0])
		c.usage(c.stderr)
		return 2
	}
	return c.exitCode(err)
}

func (c *cli) exitCode(err error) int {
	var ue usageError
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.As(err, &ue):
		if ue.msg != "" {
			fmt.Fprintln(c.stderr, "error:", ue.msg)
		}
		return 2
	case errors.Is(err, errFailed):
		return 1
	}
	fmt.Fprintln(c.stderr, "error:", err)
	return 1
}

func (c *cli) usage(w io.Writer) {
	fmt.Fprint(w, `Proposarr proposes new movies and series for Radarr and Sonarr.

Usage:
  proposarr serve [--listen ADDR]   run the web UI and HTTP API (default :8585)
  proposarr run --kind movies|series [--vibe TEXT] [--picks N] [--model M] [--effort E] [--json] [--refresh] [--add]
  proposarr add --kind movies|series --tmdb ID [--quality-profile NAME|ID] [--root-folder PATH]
  proposarr check            test every configured connection
  proposarr validate-token   probe the Claude credential with a short model call
  proposarr version

Every command accepts --config PATH. Without it, $PROPOSARR_CONFIG or ./proposarr.yaml is used.
Run "proposarr <command> -h" for a command's flags.
`)
}

func (c *cli) newFlags(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("proposarr "+name, flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	path := fs.String("config", "", "config file (default $PROPOSARR_CONFIG, else ./proposarr.yaml if present)")
	return fs, path
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageError{}
	}
	if fs.NArg() > 0 {
		return usageError{fmt.Sprintf("unexpected argument %q", fs.Arg(0))}
	}
	return nil
}

func parseKind(s string) (media.Kind, error) {
	if s == "" {
		return "", usageError{"--kind is required (movies or series)"}
	}
	k, err := media.ParseKind(s)
	if err != nil {
		return "", usageError{err.Error()}
	}
	return k, nil
}

// resolveConfigPath picks the config file: --config, then PROPOSARR_CONFIG
// (both must exist), then ./proposarr.yaml when present, else none.
func resolveConfigPath(flagPath string, getenv func(string) string) (string, error) {
	if flagPath != "" {
		if _, err := os.Stat(flagPath); err != nil {
			return "", fmt.Errorf("--config: %w", err)
		}
		return flagPath, nil
	}
	if p := getenv("PROPOSARR_CONFIG"); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("PROPOSARR_CONFIG: %w", err)
		}
		return p, nil
	}
	if _, err := os.Stat("proposarr.yaml"); err == nil {
		return "proposarr.yaml", nil
	}
	return "", nil
}

func (c *cli) loadConfig(flagPath string) (config.Config, error) {
	path, err := resolveConfigPath(flagPath, c.getenv)
	if err != nil {
		return config.Config{}, err
	}
	return config.Load(path, c.getenv)
}
