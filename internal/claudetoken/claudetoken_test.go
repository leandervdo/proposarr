package claudetoken

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/leandervdo/proposarr/internal/agent"
)

func TestProbeOK(t *testing.T) {
	f := &agent.Fake{Result: agent.Result{Text: "OK"}}
	env := []string{"CLAUDE_CODE_OAUTH_TOKEN=tok"}
	if err := Probe(context.Background(), f, env); err != nil {
		t.Fatalf("probe: %v", err)
	}
	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls: %d", len(calls))
	}
	c := calls[0]
	if c.Model != "haiku" || c.Prompt != probePrompt || c.Timeout != probeTimeout || len(c.Env) != 1 || c.Dir == "" {
		t.Fatalf("options: %+v", c)
	}
	if _, err := os.Stat(c.Dir); !os.IsNotExist(err) {
		t.Fatalf("probe dir not removed: %v", err)
	}
}

func TestProbeWrongReply(t *testing.T) {
	f := &agent.Fake{Result: agent.Result{Text: strings.Repeat("nope ", 100)}}
	err := Probe(context.Background(), f, nil)
	if err == nil || !strings.Contains(err.Error(), "unexpected reply") {
		t.Fatalf("got %v", err)
	}
	if len(err.Error()) > 260 {
		t.Fatalf("reply not truncated: %d chars", len(err.Error()))
	}
}

func TestProbeErrorPassthrough(t *testing.T) {
	f := &agent.Fake{Err: &agent.SessionLimitError{Message: "resets 3pm"}}
	err := Probe(context.Background(), f, nil)
	var sl *agent.SessionLimitError
	if !errors.As(err, &sl) {
		t.Fatalf("want SessionLimitError, got %v", err)
	}
}
