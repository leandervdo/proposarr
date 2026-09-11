package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/leandervdo/proposarr/internal/agent"
	"github.com/leandervdo/proposarr/internal/claudetoken"
)

func (c *cli) tokenCmd(ctx context.Context, args []string) error {
	fs, cfgPath := c.newFlags("validate-token")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	cfg, err := c.loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	d := buildDeps(cfg)
	env, name, err := d.agentEnv()
	if err != nil {
		return err
	}
	if name == "" {
		fmt.Fprintln(c.stderr, "No CLAUDE_CODE_OAUTH_TOKEN or ANTHROPIC_API_KEY configured; probing the local claude login.")
	} else {
		fmt.Fprintf(c.stderr, "Probing %s with a one-line claude run...\n", name)
	}

	err = claudetoken.Probe(ctx, agent.Claude{Bin: cfg.Claude.Bin}, env)
	var sl *agent.SessionLimitError
	switch {
	case errors.As(err, &sl):
		fmt.Fprintln(c.stdout, "credential valid but the session limit is hit")
		return nil
	case err != nil:
		return err
	}
	fmt.Fprintln(c.stdout, "ok")
	return nil
}
