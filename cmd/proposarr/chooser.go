package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/request"
)

// terminalChooser asks on the terminal. It never picks a quality profile on its
// own: Enter only accepts the profile chosen for the previous title of the same app.
type terminalChooser struct {
	in          *bufio.Reader
	out         io.Writer
	lastProfile map[string]int    // app -> quality profile id
	lastFolder  map[string]string // app -> root folder path
}

func newTerminalChooser(in *bufio.Reader, out io.Writer) *terminalChooser {
	return &terminalChooser{in: in, out: out, lastProfile: map[string]int{}, lastFolder: map[string]string{}}
}

func (t *terminalChooser) ChooseQualityProfile(ctx context.Context, item request.Item, app string, profiles []arr.QualityProfile) (arr.QualityProfile, error) {
	names := make([]string, len(profiles))
	def := -1
	for i, p := range profiles {
		names[i] = p.Name
		if id, ok := t.lastProfile[app]; ok && id == p.ID {
			def = i
		}
	}
	i, err := t.choose(ctx, fmt.Sprintf("Quality profile for %s in %s:", item.Label(), app), names, names, def)
	if err != nil {
		return arr.QualityProfile{}, err
	}
	t.lastProfile[app] = profiles[i].ID
	return profiles[i], nil
}

func (t *terminalChooser) ChooseRootFolder(ctx context.Context, item request.Item, app string, folders []arr.RootFolder) (arr.RootFolder, error) {
	paths := make([]string, len(folders))
	labels := make([]string, len(folders))
	def := -1
	for i, f := range folders {
		paths[i] = f.Path
		labels[i] = f.Path
		if f.FreeSpace > 0 {
			labels[i] = fmt.Sprintf("%s (%.1f GB free)", f.Path, float64(f.FreeSpace)/1e9)
		}
		if p, ok := t.lastFolder[app]; ok && p == f.Path {
			def = i
		}
	}
	i, err := t.choose(ctx, fmt.Sprintf("Root folder for %s in %s:", item.Label(), app), paths, labels, def)
	if err != nil {
		return arr.RootFolder{}, err
	}
	t.lastFolder[app] = folders[i].Path
	return folders[i], nil
}

// ChooseFallback asks what to do when Radarr finds releases but none fit the
// profile. Enter switches, like the web UI's suggestion. It is only asked when
// profiles are ranked below the chosen one.
func (t *terminalChooser) ChooseFallback(ctx context.Context, item request.Item, profile arr.QualityProfile) (request.Fallback, error) {
	names := []string{string(request.FallbackSwitch), string(request.FallbackWait)}
	labels := []string{"switch to the next lower-ranked quality profile that finds it", "keep waiting for " + profile.Name}
	i, err := t.choose(ctx, fmt.Sprintf("If nothing fits %s for %s:", profile.Name, item.Label()), names, labels, 0)
	if err != nil {
		return "", err
	}
	return request.Fallback(names[i]), nil
}

// choose lists labels and reads a number or a name until one is valid.
// def < 0 means there is no Enter default.
func (t *terminalChooser) choose(ctx context.Context, header string, names, labels []string, def int) (int, error) {
	fmt.Fprintln(t.out, header)
	for i, l := range labels {
		fmt.Fprintf(t.out, "  %d) %s\n", i+1, l)
	}
	hint := "q = stop"
	if def >= 0 {
		hint = fmt.Sprintf("Enter = %s, q = stop", names[def])
	}
	for {
		fmt.Fprintf(t.out, "Choose 1-%d [%s]: ", len(names), hint)
		line, err := t.readLine(ctx)
		if err != nil {
			return -1, err
		}
		switch {
		case line == "":
			if def >= 0 {
				return def, nil
			}
			fmt.Fprintln(t.out, "A choice is required.")
			continue
		case strings.EqualFold(line, "q"):
			return -1, request.ErrCancelled
		}
		if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= len(names) {
			return n - 1, nil
		}
		for i, name := range names {
			if strings.EqualFold(name, line) {
				return i, nil
			}
		}
		fmt.Fprintf(t.out, "Enter a number from 1 to %d or a name from the list.\n", len(names))
	}
}

type answer int

const (
	answerNo answer = iota
	answerYes
	answerQuit
)

// confirm asks a y/N/q question. EOF counts as quit.
func (t *terminalChooser) confirm(ctx context.Context, prompt string) (answer, error) {
	for {
		fmt.Fprint(t.out, prompt)
		line, err := t.readLine(ctx)
		if errors.Is(err, request.ErrCancelled) {
			return answerQuit, nil
		}
		if err != nil {
			return answerQuit, err
		}
		switch strings.ToLower(line) {
		case "y", "yes":
			return answerYes, nil
		case "", "n", "no":
			return answerNo, nil
		case "q", "quit":
			return answerQuit, nil
		}
		fmt.Fprintln(t.out, "Answer y, n or q.")
	}
}

// readLine returns the trimmed next line. EOF with nothing read is ErrCancelled.
// The read runs in a goroutine so a cancelled context is not stuck on stdin.
func (t *terminalChooser) readLine(ctx context.Context) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := t.in.ReadString('\n')
		ch <- result{line, err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		line := strings.TrimSpace(r.line)
		if r.err != nil {
			if errors.Is(r.err, io.EOF) {
				if line != "" {
					return line, nil
				}
				return "", request.ErrCancelled
			}
			return "", r.err
		}
		return line, nil
	}
}
