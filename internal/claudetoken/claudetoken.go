// Package claudetoken checks that a Claude credential works by running a
// trivial prompt through the claude binary.
package claudetoken

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/leandervdo/proposarr/internal/agent"
)

const (
	probePrompt  = "reply with the single word: ok"
	probeTimeout = 45 * time.Second
)

// Probe runs the check with env, built by agent.BuildEnv.
func Probe(ctx context.Context, p agent.Provider, env []string) error {
	dir, err := os.MkdirTemp("", "proposarr-probe-*")
	if err != nil {
		return fmt.Errorf("token probe: %w", err)
	}
	defer os.RemoveAll(dir)

	res, err := p.Run(ctx, agent.Options{
		Model:   "haiku",
		Prompt:  probePrompt,
		Env:     env,
		Timeout: probeTimeout,
		Dir:     dir,
	})
	if err != nil {
		return fmt.Errorf("token probe: %w", err)
	}
	reply := strings.TrimSpace(res.Text)
	if strings.Contains(strings.ToLower(reply), "ok") {
		return nil
	}
	if len(reply) > 200 {
		reply = reply[:200]
	}
	return fmt.Errorf("token probe: unexpected reply %q", reply)
}
