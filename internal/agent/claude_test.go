package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const sampleJSON = `{"type":"result","subtype":"success","is_error":false,"duration_ms":2618,"num_turns":2,"result":"{\"picks\":[{\"title\":\"Blade Runner\",\"year\":1982}]}","structured_output":{"picks":[{"title":"Blade Runner","year":1982}]},"session_id":"f05c3268-6790","total_cost_usd":0.01727,"usage":{"input_tokens":10,"cache_creation_input_tokens":7525,"cache_read_input_tokens":3,"output_tokens":249},"stop_reason":"tool_use","terminal_reason":"completed","permission_denials":[]}`

func TestCLIArgsMinimal(t *testing.T) {
	got := cliArgs(Options{Prompt: "secret prompt"})
	want := []string{"--print", "--output-format", "json", "--tools", "", "--permission-mode", "dontAsk",
		"--strict-mcp-config", "--no-session-persistence", "--disable-slash-commands"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args\n got %q\nwant %q", got, want)
	}
}

func TestCLIArgsFull(t *testing.T) {
	got := cliArgs(Options{
		Model:         "claude-sonnet-5",
		Effort:        "medium",
		SystemPrompt:  "sys",
		Prompt:        "secret prompt",
		JSONSchema:    `{"type":"object"}`,
		MCPConfigPath: "/run/mcp.json",
		AllowedTools:  []string{"mcp__proposarr__a", "mcp__proposarr__b"},
		MaxBudgetUSD:  0.5,
	})
	want := []string{"--print", "--output-format", "json", "--model", "claude-sonnet-5", "--effort", "medium",
		"--tools", "", "--permission-mode", "dontAsk", "--strict-mcp-config", "--no-session-persistence",
		"--disable-slash-commands", "--system-prompt", "sys", "--json-schema", `{"type":"object"}`,
		"--mcp-config", "/run/mcp.json", "--allowedTools", "mcp__proposarr__a,mcp__proposarr__b",
		"--max-budget-usd", "0.5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args\n got %q\nwant %q", got, want)
	}
	for _, a := range got {
		if strings.Contains(a, "secret prompt") {
			t.Fatal("prompt leaked into argv")
		}
	}
}

// fakeBin writes an executable shell script and returns its path.
func fakeBin(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunSuccess(t *testing.T) {
	bin := fakeBin(t, `input=$(cat)
[ "$input" = "hello prompt" ] || { echo "bad stdin: $input" >&2; exit 3; }
for a in "$@"; do [ "$a" = "hello prompt" ] && { echo "prompt in argv" >&2; exit 4; }; done
[ "$(pwd -P)" = "$EXPECT_DIR" ] || { echo "bad dir $(pwd -P)" >&2; exit 5; }
printf '%s' "$FAKE_JSON"
`)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "FAKE_JSON="+sampleJSON, "EXPECT_DIR="+dir)

	res, err := Claude{Bin: bin}.Run(context.Background(), Options{Prompt: "hello prompt", Dir: dir, Env: env})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.NumTurns != 2 || res.SessionID != "f05c3268-6790" || res.CostUSD != 0.01727 {
		t.Fatalf("result fields: %+v", res)
	}
	if res.Usage != (Usage{InputTokens: 10, OutputTokens: 249, CacheCreationInputTokens: 7525, CacheReadInputTokens: 3}) {
		t.Fatalf("usage: %+v", res.Usage)
	}
	if string(res.Structured) != `{"picks":[{"title":"Blade Runner","year":1982}]}` {
		t.Fatalf("structured: %s", res.Structured)
	}
	if !strings.Contains(res.Text, "Blade Runner") || res.Duration <= 0 {
		t.Fatalf("text/duration: %+v", res)
	}
}

func TestRunNullStructured(t *testing.T) {
	bin := fakeBin(t, `cat >/dev/null; printf '%s' '{"type":"result","subtype":"success","is_error":false,"result":"hi","structured_output":null}'`)
	res, err := Claude{Bin: bin}.Run(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Structured != nil || res.Text != "hi" {
		t.Fatalf("got %+v", res)
	}
}

func TestRunIsError(t *testing.T) {
	bin := fakeBin(t, `cat >/dev/null; printf '%s' '{"type":"result","subtype":"error_max_budget_usd","is_error":true,"result":"budget exceeded"}'; exit 1`)
	_, err := Claude{Bin: bin}.Run(context.Background(), Options{})
	var ce *CLIError
	if !errors.As(err, &ce) {
		t.Fatalf("want CLIError, got %v", err)
	}
	if ce.Subtype != "error_max_budget_usd" || ce.Message != "budget exceeded" || ce.ExitCode != 1 {
		t.Fatalf("got %+v", ce)
	}
}

func TestRunSessionLimitStderr(t *testing.T) {
	bin := fakeBin(t, `cat >/dev/null; echo "You've hit your session limit · resets 3pm" >&2; exit 1`)
	_, err := Claude{Bin: bin}.Run(context.Background(), Options{})
	var sl *SessionLimitError
	if !errors.As(err, &sl) || !strings.Contains(sl.Message, "resets 3pm") {
		t.Fatalf("want SessionLimitError, got %v", err)
	}
}

func TestRunSessionLimitResult(t *testing.T) {
	bin := fakeBin(t, `cat >/dev/null; printf '%s' '{"type":"result","subtype":"success","is_error":true,"result":"Youve hit your usage limit"}'; exit 1`)
	_, err := Claude{Bin: bin}.Run(context.Background(), Options{})
	var sl *SessionLimitError
	if !errors.As(err, &sl) {
		t.Fatalf("want SessionLimitError, got %v", err)
	}
}

func TestRunNonZeroExit(t *testing.T) {
	bin := fakeBin(t, `cat >/dev/null; echo boom >&2; exit 2`)
	_, err := Claude{Bin: bin}.Run(context.Background(), Options{})
	var ce *CLIError
	if !errors.As(err, &ce) || ce.ExitCode != 2 || ce.Stderr != "boom" {
		t.Fatalf("got %v", err)
	}
}

func TestRunMissingBinary(t *testing.T) {
	_, err := Claude{Bin: filepath.Join(t.TempDir(), "nope")}.Run(context.Background(), Options{})
	var ce *CLIError
	if !errors.As(err, &ce) || ce.Message == "" {
		t.Fatalf("got %v", err)
	}
}

func TestRunTimeout(t *testing.T) {
	bin := fakeBin(t, `exec sleep 5`)
	start := time.Now()
	_, err := Claude{Bin: bin}.Run(context.Background(), Options{Timeout: 200 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout, got %v", err)
	}
	if time.Since(start) > 4*time.Second {
		t.Fatalf("timeout took %s", time.Since(start))
	}
}

func TestStructuredJSON(t *testing.T) {
	cases := []struct {
		name string
		r    Result
		want string
		err  bool
	}{
		{"structured wins", Result{Structured: []byte(`{"a":1}`), Text: "nope"}, `{"a":1}`, false},
		{"plain", Result{Text: ` {"a":2} `}, `{"a":2}`, false},
		{"json fence", Result{Text: "```json\n{\"a\":3}\n```"}, `{"a":3}`, false},
		{"bare fence", Result{Text: "```\n[1,2]\n```\n"}, `[1,2]`, false},
		{"invalid", Result{Text: "sorry, no"}, "", true},
		{"empty", Result{}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := StructuredJSON(c.r)
			if (err != nil) != c.err {
				t.Fatalf("err = %v", err)
			}
			if !c.err && string(got) != c.want {
				t.Fatalf("got %s want %s", got, c.want)
			}
		})
	}
}

func TestBuildEnv(t *testing.T) {
	base := []string{"PATH=/bin", "ANTHROPIC_API_KEY=old", "CLAUDE_CODE_OAUTH_TOKEN=old", "HOME=/h"}

	if got := BuildEnv(base, "", ""); !reflect.DeepEqual(got, base) {
		t.Fatalf("empty name changed env: %q", got)
	}
	got := BuildEnv(base, "CLAUDE_CODE_OAUTH_TOKEN", "new")
	want := []string{"PATH=/bin", "HOME=/h", "CLAUDE_CODE_OAUTH_TOKEN=new"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	got = BuildEnv(base, "ANTHROPIC_API_KEY", "k")
	want = []string{"PATH=/bin", "HOME=/h", "ANTHROPIC_API_KEY=k"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFakeRecordsCalls(t *testing.T) {
	f := &Fake{Result: Result{Text: "x"}}
	if r, _ := f.Run(context.Background(), Options{Model: "m1"}); r.Text != "x" {
		t.Fatal("canned result not returned")
	}
	f.Fn = func(o Options) (Result, error) { return Result{Text: o.Model}, nil }
	if r, _ := f.Run(context.Background(), Options{Model: "m2"}); r.Text != "m2" {
		t.Fatal("Fn not used")
	}
	if c := f.Calls(); len(c) != 2 || c[1].Model != "m2" {
		t.Fatalf("calls: %+v", c)
	}
}
