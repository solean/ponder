// Package ai generates deck content through locally installed, subscription-
// authenticated AI CLIs. Credentials stay under each CLI's own login.
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CLIProvider locates and drives the `claude` binary. Safe for concurrent
// use; discovery runs once and is cached.
type CLIProvider struct {
	once    sync.Once
	cliPath string
	version string
	detail  string
}

// lookupCLI finds the claude binary. The desktop app is typically launched
// from Finder with a minimal PATH, so LookPath alone is not enough — probe
// the common install locations too.
func (p *CLIProvider) lookupCLI() {
	if path, err := exec.LookPath("claude"); err == nil {
		p.cliPath = path
		return
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".claude", "local", "claude"),
		filepath.Join(home, ".local", "bin", "claude"),
		filepath.Join(home, ".bun", "bin", "claude"),
		"/opt/homebrew/bin/claude",
		"/usr/local/bin/claude",
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			p.cliPath = candidate
			return
		}
	}
	p.detail = "Claude Code CLI not found. Install it and sign in with your Claude subscription to enable AI features."
}

// Status checks installation and login separately. Authentication is checked
// on every call so signing in or token expiry is reflected without a restart.
func (p *CLIProvider) Status(ctx context.Context) ProviderStatus {
	p.once.Do(func() {
		p.lookupCLI()
		if p.cliPath == "" {
			return
		}
		versionCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(versionCtx, p.cliPath, "--version").CombinedOutput()
		if err != nil {
			p.detail = fmt.Sprintf(
				"Found Claude Code CLI at %s, but it could not run. Update or reinstall Claude Code. Details: %s",
				p.cliPath,
				commandSummary(err, out),
			)
			return
		}
		p.version = strings.TrimSpace(string(out))
	})

	status := ProviderStatus{
		ID:        ProviderClaude,
		Name:      "Claude Code",
		Installed: p.cliPath != "",
		CLIPath:   p.cliPath,
		Version:   p.version,
		Models:    providerModels(ProviderClaude),
		Detail:    p.detail,
	}
	if p.cliPath == "" || p.version == "" {
		return status
	}

	authCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, commandErr := exec.CommandContext(authCtx, p.cliPath, "auth", "status", "--json").CombinedOutput()
	var auth struct {
		LoggedIn         bool   `json:"loggedIn"`
		AuthMethod       string `json:"authMethod"`
		SubscriptionType string `json:"subscriptionType"`
	}
	if json.Unmarshal(out, &auth) == nil {
		if !auth.LoggedIn {
			status.Detail = "Claude Code is installed but not signed in. Run `claude auth login`."
			return status
		}
		status.Authenticated = true
		status.Available = true
		status.AuthMethod = strings.TrimSpace(auth.SubscriptionType)
		if status.AuthMethod == "" {
			status.AuthMethod = strings.TrimSpace(auth.AuthMethod)
		}
		status.Detail = "Ready to generate with your existing Claude login."
		return status
	}
	if commandErr != nil {
		status.Detail = "Claude Code is installed but its login could not be verified. Run `claude auth login`."
		if detail := truncate(string(out), 300); detail != "" {
			status.Detail += " Details: " + detail
		}
		return status
	}
	status.Detail = "Claude Code returned an unreadable authentication status. Update Claude Code and retry."
	return status
}

// claudeUsagePayload matches Claude Code's aggregate result and message-delta
// accounting fields.
type claudeUsagePayload struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheWriteInputTokens int64 `json:"cache_creation_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	OutputTokenDetails    struct {
		ThinkingTokens int64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

func (u claudeUsagePayload) tokenUsage() TokenUsage {
	return TokenUsage{
		InputTokens:           u.InputTokens,
		CachedInputTokens:     u.CachedInputTokens,
		CacheWriteInputTokens: u.CacheWriteInputTokens,
		OutputTokens:          u.OutputTokens,
		ReasoningOutputTokens: u.OutputTokenDetails.ThinkingTokens,
	}
}

// streamLine is the subset of Claude Code's stream-json output we care about.
type streamLine struct {
	Type    string             `json:"type"`
	Subtype string             `json:"subtype"`
	IsError bool               `json:"is_error"`
	Result  string             `json:"result"`
	Usage   claudeUsagePayload `json:"usage"`
	Event   struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
		Usage claudeUsagePayload `json:"usage"`
	} `json:"event"`
}

// Generate runs `claude -p` with the given prompt, invoking onDelta for each
// streamed text fragment, and returns the final response text. onDelta may be
// nil. Cancelling ctx kills the CLI process.
func (p *CLIProvider) Generate(ctx context.Context, model, prompt string, onDelta func(string)) (GenerationResult, error) {
	status := p.Status(ctx)
	if err := ctx.Err(); err != nil {
		return GenerationResult{}, err
	}
	if !status.Available {
		return GenerationResult{}, errors.New(status.Detail)
	}
	if model == "" {
		model = DefaultClaudeModel
	}

	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
		"--model", model,
		// Web search grounds card text and current-metagame claims; everything
		// else (shell, file edits) is irrelevant to primer generation.
		"--allowedTools", "WebSearch,WebFetch",
		"--disallowedTools", "Bash,Edit,Write,NotebookEdit,Task",
	}
	cmd := exec.CommandContext(ctx, p.cliPath, args...)
	// Run from a neutral directory so the CLI doesn't pick up project context
	// (CLAUDE.md, local settings) from wherever the app was launched.
	cmd.Dir = os.TempDir()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return GenerationResult{}, fmt.Errorf("claude cli stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return GenerationResult{}, fmt.Errorf("start claude cli: %w", err)
	}

	var (
		result    string
		gotResult bool
		runErr    error
		usage     TokenUsage
	)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var parsed streamLine
		if err := json.Unmarshal(line, &parsed); err != nil {
			continue
		}
		switch parsed.Type {
		case "stream_event":
			usage = mergeTokenUsage(usage, parsed.Event.Usage.tokenUsage())
			if parsed.Event.Type == "content_block_delta" && parsed.Event.Delta.Type == "text_delta" && onDelta != nil {
				onDelta(parsed.Event.Delta.Text)
			}
		case "result":
			gotResult = true
			usage = mergeTokenUsage(usage, parsed.Usage.tokenUsage())
			if parsed.IsError {
				// The CLI can report subtype "success" alongside is_error
				// (e.g. auth failures), so only mention informative subtypes.
				message := truncate(parsed.Result, 500)
				if message == "" {
					message = "unknown error"
				}
				if parsed.Subtype != "" && parsed.Subtype != "success" {
					runErr = fmt.Errorf("claude cli error (%s): %s", parsed.Subtype, message)
				} else {
					runErr = fmt.Errorf("claude cli error: %s", message)
				}
			} else {
				result = parsed.Result
			}
		}
	}
	if scanErr := scanner.Err(); scanErr != nil && runErr == nil {
		runErr = fmt.Errorf("read claude cli output: %w", scanErr)
	}

	if err := cmd.Wait(); err != nil && runErr == nil {
		if ctx.Err() != nil {
			return GenerationResult{Usage: usage}, ctx.Err()
		}
		runErr = fmt.Errorf("claude cli failed: %v: %s", err, truncate(stderr.String(), 500))
	}
	if runErr != nil {
		return GenerationResult{Usage: usage}, actionableGenerationError(ProviderClaude, runErr)
	}
	if !gotResult || strings.TrimSpace(result) == "" {
		return GenerationResult{Usage: usage}, fmt.Errorf("claude cli produced no result: %s", truncate(stderr.String(), 500))
	}
	return GenerationResult{Content: result, Usage: usage}, nil
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
