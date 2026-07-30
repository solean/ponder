package ai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeCLI(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-cli")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o755); err != nil {
		t.Fatalf("write fake CLI: %v", err)
	}
	return path
}

func TestClaudeStatusRechecksSubscriptionLogin(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "logged-in")
	cliPath := writeFakeCLI(t, fmt.Sprintf(`
if [ "$1" = "auth" ]; then
  if [ -f %q ]; then
    printf '%%s\n' '{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"pro"}'
  else
    printf '%%s\n' '{"loggedIn":false}'
    exit 1
  fi
  exit 0
fi
exit 2
`, statePath))

	provider := &CLIProvider{cliPath: cliPath, version: "claude fake"}
	provider.once.Do(func() {})

	loggedOut := provider.Status(context.Background())
	if !loggedOut.Installed || loggedOut.Authenticated || loggedOut.Available {
		t.Fatalf("logged-out status = %+v, want installed but unavailable", loggedOut)
	}
	if !strings.Contains(loggedOut.Detail, "claude auth login") {
		t.Fatalf("logged-out detail = %q, want login instruction", loggedOut.Detail)
	}

	if err := os.WriteFile(statePath, nil, 0o644); err != nil {
		t.Fatalf("mark fake login active: %v", err)
	}
	loggedIn := provider.Status(context.Background())
	if !loggedIn.Authenticated || !loggedIn.Available {
		t.Fatalf("logged-in status = %+v, want available", loggedIn)
	}
	if loggedIn.AuthMethod != "pro" {
		t.Fatalf("auth method = %q, want pro", loggedIn.AuthMethod)
	}
}

func TestCodexGenerateUsesSelectedModelAndParsesJSONL(t *testing.T) {
	tmpDir := t.TempDir()
	argsPath := filepath.Join(tmpDir, "args")
	promptPath := filepath.Join(tmpDir, "prompt")
	cliPath := writeFakeCLI(t, fmt.Sprintf(`
case "$1" in
  login)
    printf '%%s\n' 'Logged in using ChatGPT'
    ;;
  debug)
    printf '%%s\n' '{"models":[{"slug":"gpt-test","display_name":"GPT Test","description":"Test model","visibility":"list"}]}'
    ;;
  --search)
    printf '%%s\n' "$@" > %q
    cat > %q
    printf '%%s\n' '{"type":"thread.started","thread_id":"thread-1"}'
    printf '%%s\n' '{"type":"item.completed","item":{"id":"item-1","type":"agent_message","text":"generated primer"}}'
    printf '%%s\n' '{"type":"turn.completed","usage":{"input_tokens":1,"cached_input_tokens":0,"output_tokens":2,"reasoning_output_tokens":0}}'
    ;;
  *)
    exit 2
    ;;
esac
`, argsPath, promptPath))

	provider := &CodexCLIProvider{cliPath: cliPath, version: "codex fake"}
	provider.once.Do(func() {})
	status := provider.Status(context.Background())
	if !status.Available || status.AuthMethod != "ChatGPT" {
		t.Fatalf("status = %+v, want ChatGPT login available", status)
	}
	if len(status.Models) != 2 || status.Models[1].ID != "gpt-test" {
		t.Fatalf("models = %+v, want default plus discovered model", status.Models)
	}

	var streamed strings.Builder
	result, err := provider.Generate(context.Background(), "gpt-test", "deck prompt", func(delta string) {
		streamed.WriteString(delta)
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Content != "generated primer" || streamed.String() != result.Content {
		t.Fatalf("result = %q, streamed = %q", result.Content, streamed.String())
	}
	if result.Usage.InputTokens != 1 || result.Usage.OutputTokens != 2 {
		t.Fatalf("usage = %+v, want 1 input and 2 output tokens", result.Usage)
	}

	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read fake CLI args: %v", err)
	}
	argsText := string(args)
	for _, expected := range []string{"exec", "--json", "--ephemeral", "--sandbox", "read-only", "--model", "gpt-test"} {
		if !strings.Contains(argsText, expected+"\n") {
			t.Errorf("CLI args missing %q: %q", expected, argsText)
		}
	}
	prompt, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read fake CLI prompt: %v", err)
	}
	if string(prompt) != "deck prompt" {
		t.Fatalf("prompt = %q, want deck prompt", prompt)
	}
}

func TestClaudeGenerateParsesAggregateAndDetailedUsage(t *testing.T) {
	cliPath := writeFakeCLI(t, `
if [ "$1" = "auth" ]; then
  printf '%s\n' '{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"pro"}'
  exit 0
fi
printf '%s\n' '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"generated primer"},"usage":{"input_tokens":9,"cache_read_input_tokens":4,"cache_creation_input_tokens":2,"output_tokens":3,"output_tokens_details":{"thinking_tokens":2}}}}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"generated primer","usage":{"input_tokens":9,"cache_read_input_tokens":4,"cache_creation_input_tokens":2,"output_tokens":6}}'
`)
	provider := &CLIProvider{cliPath: cliPath, version: "claude fake"}
	provider.once.Do(func() {})

	result, err := provider.Generate(context.Background(), "opus", "deck prompt", nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	want := TokenUsage{
		InputTokens:           9,
		CachedInputTokens:     4,
		CacheWriteInputTokens: 2,
		OutputTokens:          6,
		ReasoningOutputTokens: 2,
	}
	if result.Content != "generated primer" || result.Usage != want {
		t.Fatalf("generation = %+v, want content and usage %+v", result, want)
	}
}

func TestAuthenticationErrorsIncludeLoginRecovery(t *testing.T) {
	err := actionableGenerationError(ProviderOpenAI, errors.New("request failed: status 401 unauthorized"))
	if err == nil || !strings.Contains(err.Error(), "codex login") {
		t.Fatalf("error = %v, want Codex login recovery", err)
	}
}

func TestGenerationModelRecordsProvider(t *testing.T) {
	if got := GenerationModel(ProviderOpenAI, "gpt-test"); got != "openai/gpt-test" {
		t.Fatalf("generation model = %q", got)
	}
	if got := GenerationModel(ProviderClaude, ""); got != "claude/opus" {
		t.Fatalf("default generation model = %q", got)
	}
}
