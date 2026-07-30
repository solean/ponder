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

// CodexCLIProvider drives the Codex CLI using its existing ChatGPT login.
type CodexCLIProvider struct {
	once       sync.Once
	modelsOnce sync.Once
	cliPath    string
	version    string
	detail     string
	models     []ModelOption
}

func (p *CodexCLIProvider) lookupCLI() {
	if path, err := exec.LookPath("codex"); err == nil {
		p.cliPath = path
		return
	}

	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", "codex"),
		filepath.Join(home, ".bun", "bin", "codex"),
		filepath.Join(home, ".volta", "bin", "codex"),
		filepath.Join(home, ".npm-global", "bin", "codex"),
		"/opt/homebrew/bin/codex",
		"/usr/local/bin/codex",
	}
	// Finder launches with a sparse PATH. Include version-managed Node installs
	// because npm's global Codex package is commonly installed there.
	if versions, err := os.ReadDir(filepath.Join(home, ".nvm", "versions", "node")); err == nil {
		for i := len(versions) - 1; i >= 0; i-- {
			if versions[i].IsDir() {
				candidates = append(candidates, filepath.Join(home, ".nvm", "versions", "node", versions[i].Name(), "bin", "codex"))
			}
		}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			p.cliPath = candidate
			return
		}
	}
	p.detail = "Codex CLI not found. Install it, run `codex login`, and choose Sign in with ChatGPT to use your OpenAI subscription."
}

func (p *CodexCLIProvider) discover() {
	p.lookupCLI()
	if p.cliPath == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, p.cliPath, "--version").CombinedOutput()
	if err != nil {
		p.detail = fmt.Sprintf(
			"Found Codex CLI at %s, but it could not run. Update or reinstall Codex CLI. Details: %s",
			p.cliPath,
			commandSummary(err, out),
		)
		return
	}
	p.version = strings.TrimSpace(string(out))
}

func (p *CodexCLIProvider) Status(ctx context.Context) ProviderStatus {
	p.once.Do(p.discover)
	status := ProviderStatus{
		ID:        ProviderOpenAI,
		Name:      "OpenAI (Codex)",
		Installed: p.cliPath != "",
		CLIPath:   p.cliPath,
		Version:   p.version,
		Models:    providerModels(ProviderOpenAI),
		Detail:    p.detail,
	}
	if p.cliPath == "" || p.version == "" {
		return status
	}

	authCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(authCtx, p.cliPath, "login", "status").CombinedOutput()
	if err != nil {
		detail := truncate(string(out), 300)
		if detail == "" {
			detail = err.Error()
		}
		status.Detail = "Codex is installed but not signed in. Run `codex login` and choose Sign in with ChatGPT."
		if detail != "" && !strings.EqualFold(detail, "not logged in") {
			status.Detail += " Details: " + detail
		}
		return status
	}

	status.Authenticated = true
	status.Available = true
	status.AuthMethod = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "Logged in using "))
	if status.AuthMethod == "" {
		status.AuthMethod = "Codex login"
	}
	status.Detail = "Ready to generate with your existing Codex login."
	status.Models = p.modelOptions()
	return status
}

type codexModelCatalog struct {
	Models []struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		Visibility  string `json:"visibility"`
	} `json:"models"`
}

func (p *CodexCLIProvider) modelOptions() []ModelOption {
	p.modelsOnce.Do(func() {
		p.models = providerModels(ProviderOpenAI)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, p.cliPath, "debug", "models", "--bundled").Output()
		if err != nil {
			return
		}
		var catalog codexModelCatalog
		if json.Unmarshal(out, &catalog) != nil {
			return
		}
		seen := map[string]bool{DefaultOpenAIModel: true}
		for _, model := range catalog.Models {
			id := strings.TrimSpace(model.Slug)
			if id == "" || seen[id] || (model.Visibility != "" && model.Visibility != "list") {
				continue
			}
			name := strings.TrimSpace(model.DisplayName)
			if name == "" {
				name = id
			}
			p.models = append(p.models, ModelOption{ID: id, Name: name, Description: strings.TrimSpace(model.Description)})
			seen[id] = true
		}
	})
	return append([]ModelOption(nil), p.models...)
}

type codexUsagePayload struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
}

func (u codexUsagePayload) tokenUsage() TokenUsage {
	return TokenUsage{
		InputTokens:           u.InputTokens,
		CachedInputTokens:     u.CachedInputTokens,
		CacheWriteInputTokens: u.CacheWriteInputTokens,
		OutputTokens:          u.OutputTokens,
		ReasoningOutputTokens: u.ReasoningOutputTokens,
	}
}

type codexStreamLine struct {
	Type    string            `json:"type"`
	Message string            `json:"message"`
	Usage   codexUsagePayload `json:"usage"`
	Error   struct {
		Message string `json:"message"`
	} `json:"error"`
	Item struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
}

// Generate runs a read-only, ephemeral Codex exec session. The CLI receives the
// prompt on stdin so large deck histories never hit command-line size limits.
func (p *CodexCLIProvider) Generate(ctx context.Context, model, prompt string, onDelta func(string)) (GenerationResult, error) {
	status := p.Status(ctx)
	if err := ctx.Err(); err != nil {
		return GenerationResult{}, err
	}
	if !status.Available {
		return GenerationResult{}, errors.New(status.Detail)
	}
	model = NormalizeModel(ProviderOpenAI, model)

	args := []string{
		"--search",
		"exec",
		"--json",
		"--ephemeral",
		"--skip-git-repo-check",
		"--ignore-user-config",
		"--ignore-rules",
		"--sandbox", "read-only",
	}
	if model != DefaultOpenAIModel {
		args = append(args, "--model", model)
	}
	args = append(args, "-")

	cmd := exec.CommandContext(ctx, p.cliPath, args...)
	cmd.Dir = os.TempDir()
	cmd.Stdin = strings.NewReader(prompt)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return GenerationResult{}, fmt.Errorf("codex cli stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return GenerationResult{}, fmt.Errorf("start codex cli: %w", err)
	}

	var result, streamed string
	var runErr error
	var usage TokenUsage
	emitText := func(text string) {
		if onDelta == nil || text == "" {
			return
		}
		if strings.HasPrefix(text, streamed) {
			onDelta(strings.TrimPrefix(text, streamed))
		} else if text != streamed {
			onDelta(text)
		}
		streamed = text
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var parsed codexStreamLine
		if json.Unmarshal(line, &parsed) != nil {
			continue
		}
		switch parsed.Type {
		case "item.updated":
			if parsed.Item.Type == "agent_message" {
				emitText(parsed.Item.Text)
			}
		case "item.completed":
			if parsed.Item.Type == "agent_message" {
				result = parsed.Item.Text
				emitText(result)
			}
		case "turn.completed":
			usage = parsed.Usage.tokenUsage()
		case "turn.failed":
			if parsed.Error.Message != "" {
				runErr = errors.New(parsed.Error.Message)
			}
		case "error":
			if parsed.Message != "" {
				runErr = errors.New(parsed.Message)
			}
		}
	}
	if scanErr := scanner.Err(); scanErr != nil && runErr == nil {
		runErr = fmt.Errorf("read codex cli output: %w", scanErr)
	}
	if err := cmd.Wait(); err != nil && runErr == nil {
		if ctx.Err() != nil {
			return GenerationResult{Usage: usage}, ctx.Err()
		}
		runErr = fmt.Errorf("codex cli failed: %s", commandFailure(err, stderr.Bytes()))
	}
	if runErr != nil {
		return GenerationResult{Usage: usage}, actionableGenerationError(ProviderOpenAI, runErr)
	}
	if strings.TrimSpace(result) == "" {
		return GenerationResult{Usage: usage}, fmt.Errorf("codex cli produced no result: %s", truncate(stderr.String(), 500))
	}
	return GenerationResult{Content: result, Usage: usage}, nil
}

func commandFailure(err error, output []byte) string {
	detail := truncate(string(output), 500)
	if detail == "" {
		return err.Error()
	}
	return err.Error() + ": " + detail
}

func commandSummary(err error, output []byte) string {
	detail := strings.TrimSpace(string(output))
	if newline := strings.IndexByte(detail, '\n'); newline >= 0 {
		detail = strings.TrimSpace(detail[:newline])
	}
	if detail == "" {
		return err.Error()
	}
	return truncate(detail, 300)
}
