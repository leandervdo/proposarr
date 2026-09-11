// Package agent runs a model through the headless claude CLI.
package agent

import (
	"context"
	"fmt"
	"time"
)

// Provider is the one-method port the pipeline depends on.
type Provider interface {
	Run(ctx context.Context, opts Options) (Result, error)
}

// Options maps one-to-one onto claude CLI flags, so argv is a pure function of it.
type Options struct {
	Model         string
	Effort        string
	SystemPrompt  string
	Prompt        string // sent on stdin, never argv
	JSONSchema    string
	MCPConfigPath string
	AllowedTools  []string
	MaxBudgetUSD  float64
	Dir           string        // working directory; an empty temp dir per run
	Env           []string      // nil inherits the process env
	Timeout       time.Duration // wall clock; 0 means DefaultTimeout
}

const DefaultTimeout = 10 * time.Minute

type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type Result struct {
	Text       string // the CLI "result" string
	Structured []byte // the CLI "structured_output" object when --json-schema was used
	NumTurns   int
	SessionID  string
	CostUSD    float64
	Usage      Usage
	Duration   time.Duration
}

// SessionLimitError means the subscription window is exhausted. Never retried.
type SessionLimitError struct{ Message string }

func (e *SessionLimitError) Error() string { return "claude session limit: " + e.Message }

// CLIError is any other failed run.
type CLIError struct {
	Subtype  string
	Message  string
	ExitCode int
	Stderr   string
}

func (e *CLIError) Error() string {
	msg := "claude run failed"
	if e.Subtype != "" {
		msg += " (" + e.Subtype + ")"
	}
	if e.ExitCode != 0 {
		msg += fmt.Sprintf(" exit %d", e.ExitCode)
	}
	if e.Message != "" {
		msg += ": " + e.Message
	} else if e.Stderr != "" {
		msg += ": " + e.Stderr
	}
	return msg
}
