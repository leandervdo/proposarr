package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Claude runs the official claude binary in print mode.
type Claude struct{ Bin string }

var sessionLimitRe = regexp.MustCompile(`(?i)you'?ve hit your (session|usage|weekly) limit`)

const (
	envOAuthToken = "CLAUDE_CODE_OAUTH_TOKEN"
	envAPIKey     = "ANTHROPIC_API_KEY"
	stderrTailMax = 2048
)

type cliJSON struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	IsError          bool            `json:"is_error"`
	Result           string          `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	NumTurns         int             `json:"num_turns"`
	SessionID        string          `json:"session_id"`
	TotalCostUSD     float64         `json:"total_cost_usd"`
	Usage            Usage           `json:"usage"`
}

// cliArgs builds argv. The prompt is never part of it; it goes to stdin.
func cliArgs(o Options) []string {
	args := []string{"--print", "--output-format", "json"}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.Effort != "" {
		args = append(args, "--effort", o.Effort)
	}
	args = append(args,
		"--tools", "",
		"--permission-mode", "dontAsk",
		"--strict-mcp-config",
		"--no-session-persistence",
		"--disable-slash-commands",
	)
	if o.SystemPrompt != "" {
		args = append(args, "--system-prompt", o.SystemPrompt)
	}
	if o.JSONSchema != "" {
		args = append(args, "--json-schema", o.JSONSchema)
	}
	if o.MCPConfigPath != "" {
		args = append(args, "--mcp-config", o.MCPConfigPath)
	}
	if len(o.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(o.AllowedTools, ","))
	}
	if o.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(o.MaxBudgetUSD, 'f', -1, 64))
	}
	return args
}

func (c Claude) Run(ctx context.Context, o Options) (Result, error) {
	bin := c.Bin
	if bin == "" {
		bin = "claude"
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, cliArgs(o)...)
	cmd.Dir = o.Dir
	cmd.Env = o.Env
	cmd.Stdin = strings.NewReader(o.Prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = 2 * time.Second

	start := time.Now()
	runErr := cmd.Run()
	dur := time.Since(start)

	switch err := ctx.Err(); {
	case errors.Is(err, context.DeadlineExceeded):
		return Result{Duration: dur}, fmt.Errorf("claude run timed out after %s", timeout)
	case err != nil:
		return Result{Duration: dur}, fmt.Errorf("claude run: %w", err)
	}

	errTail := tail(stderr.String(), stderrTailMax)
	var parsed cliJSON
	perr := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &parsed)

	if perr == nil && sessionLimitRe.MatchString(parsed.Result) {
		return Result{Duration: dur}, &SessionLimitError{Message: strings.TrimSpace(parsed.Result)}
	}
	if sessionLimitRe.MatchString(stderr.String()) {
		return Result{Duration: dur}, &SessionLimitError{Message: errTail}
	}

	exitCode := 0
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		exitCode = exitErr.ExitCode()
	}

	if perr != nil {
		ce := &CLIError{ExitCode: exitCode, Stderr: errTail}
		switch {
		case runErr != nil && exitErr == nil:
			ce.Message = runErr.Error()
		case runErr == nil:
			ce.Message = "unparseable output: " + perr.Error()
		}
		return Result{Duration: dur}, ce
	}

	res := Result{
		Text:      parsed.Result,
		NumTurns:  parsed.NumTurns,
		SessionID: parsed.SessionID,
		CostUSD:   parsed.TotalCostUSD,
		Usage:     parsed.Usage,
		Duration:  dur,
	}
	if so := bytes.TrimSpace(parsed.StructuredOutput); len(so) > 0 && string(so) != "null" {
		res.Structured = append([]byte(nil), so...)
	}

	if parsed.IsError {
		return res, &CLIError{Subtype: parsed.Subtype, Message: parsed.Result, ExitCode: exitCode}
	}
	if runErr != nil {
		return res, &CLIError{Subtype: parsed.Subtype, ExitCode: exitCode, Stderr: errTail}
	}
	return res, nil
}

// StructuredJSON returns the structured output, falling back to the result
// text with a surrounding code fence removed.
func StructuredJSON(r Result) ([]byte, error) {
	if len(r.Structured) > 0 {
		return r.Structured, nil
	}
	s := strings.TrimSpace(r.Text)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if i := strings.IndexByte(s, '\n'); i >= 0 && isLangTag(s[:i]) {
			s = s[i+1:]
		}
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
	}
	if s == "" {
		return nil, errors.New("claude returned an empty result")
	}
	if !json.Valid([]byte(s)) {
		return nil, fmt.Errorf("claude result is not valid JSON: %q", tail(s, 200))
	}
	return []byte(s), nil
}

func isLangTag(s string) bool {
	s = strings.TrimSpace(s)
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// BuildEnv returns base with exactly one Claude credential set. An empty name
// leaves base untouched so the binary's own login is used.
func BuildEnv(base []string, name, value string) []string {
	if name == "" {
		return base
	}
	out := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if strings.HasPrefix(kv, envOAuthToken+"=") || strings.HasPrefix(kv, envAPIKey+"=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, name+"="+value)
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = s[len(s)-n:]
	}
	return s
}
