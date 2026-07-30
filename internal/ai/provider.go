package ai

import (
	"context"
	"fmt"
	"strings"
)

const (
	ProviderClaude = "claude"
	ProviderOpenAI = "openai"

	DefaultProvider    = ProviderClaude
	DefaultClaudeModel = "opus"
	DefaultOpenAIModel = "default"
)

// TokenUsage is the provider-reported token accounting for one generation.
// Cached and reasoning counts can be subsets of input/output for some
// providers, so callers must not blindly add every field into one total.
type TokenUsage struct {
	InputTokens           int64 `json:"inputTokens"`
	CachedInputTokens     int64 `json:"cachedInputTokens"`
	CacheWriteInputTokens int64 `json:"cacheWriteInputTokens"`
	OutputTokens          int64 `json:"outputTokens"`
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
}

func (u TokenUsage) HasTokens() bool {
	return u.InputTokens > 0 ||
		u.CachedInputTokens > 0 ||
		u.CacheWriteInputTokens > 0 ||
		u.OutputTokens > 0 ||
		u.ReasoningOutputTokens > 0
}
func mergeTokenUsage(current, next TokenUsage) TokenUsage {
	current.InputTokens = max(current.InputTokens, next.InputTokens)
	current.CachedInputTokens = max(current.CachedInputTokens, next.CachedInputTokens)
	current.CacheWriteInputTokens = max(current.CacheWriteInputTokens, next.CacheWriteInputTokens)
	current.OutputTokens = max(current.OutputTokens, next.OutputTokens)
	current.ReasoningOutputTokens = max(current.ReasoningOutputTokens, next.ReasoningOutputTokens)
	return current
}

type GenerationResult struct {
	Content string
	Usage   TokenUsage
}

// ModelOption is a model exposed by one subscription-backed CLI.
type ModelOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ProviderStatus reports installation and authentication separately so the UI
// can distinguish a missing CLI from a login that needs attention.
type ProviderStatus struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Installed     bool          `json:"installed"`
	Authenticated bool          `json:"authenticated"`
	Available     bool          `json:"available"`
	CLIPath       string        `json:"cliPath,omitempty"`
	Version       string        `json:"version,omitempty"`
	AuthMethod    string        `json:"authMethod,omitempty"`
	Detail        string        `json:"detail,omitempty"`
	Models        []ModelOption `json:"models"`
}

// Status is the selected provider plus every provider's setup state.
type Status struct {
	Available     bool             `json:"available"`
	Provider      string           `json:"provider"`
	ProviderName  string           `json:"providerName"`
	Model         string           `json:"model"`
	Installed     bool             `json:"installed"`
	Authenticated bool             `json:"authenticated"`
	CLIPath       string           `json:"cliPath,omitempty"`
	Version       string           `json:"version,omitempty"`
	AuthMethod    string           `json:"authMethod,omitempty"`
	Detail        string           `json:"detail,omitempty"`
	Providers     []ProviderStatus `json:"providers"`
}

type provider interface {
	Status(context.Context) ProviderStatus
	Generate(context.Context, string, string, func(string)) (GenerationResult, error)
}

// Service selects between local CLIs without storing subscription credentials.
type Service struct {
	providers map[string]provider
}

func NewService() *Service {
	return &Service{providers: map[string]provider{
		ProviderClaude: &CLIProvider{},
		ProviderOpenAI: &CodexCLIProvider{},
	}}
}

func NormalizeProvider(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ProviderOpenAI:
		return ProviderOpenAI
	case ProviderClaude:
		return ProviderClaude
	default:
		return DefaultProvider
	}
}

func DefaultModelForProvider(providerID string) string {
	if NormalizeProvider(providerID) == ProviderOpenAI {
		return DefaultOpenAIModel
	}
	return DefaultClaudeModel
}

// NormalizeModel preserves explicit model IDs so newer CLI models work before
// the app knows about them. An empty value selects the provider default.
func NormalizeModel(providerID, model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return DefaultModelForProvider(providerID)
	}
	return model
}

func (s *Service) Status(ctx context.Context, providerID, model string) Status {
	providerID = NormalizeProvider(providerID)
	model = NormalizeModel(providerID, model)

	providerStatuses := make([]ProviderStatus, 0, 2)
	var selected ProviderStatus
	for _, id := range []string{ProviderClaude, ProviderOpenAI} {
		status := s.providers[id].Status(ctx)
		providerStatuses = append(providerStatuses, status)
		if id == providerID {
			selected = status
		}
	}

	return Status{
		Available:     selected.Available,
		Provider:      providerID,
		ProviderName:  selected.Name,
		Model:         model,
		Installed:     selected.Installed,
		Authenticated: selected.Authenticated,
		CLIPath:       selected.CLIPath,
		Version:       selected.Version,
		AuthMethod:    selected.AuthMethod,
		Detail:        selected.Detail,
		Providers:     providerStatuses,
	}
}
func (s *Service) ProviderStatus(ctx context.Context, providerID string) ProviderStatus {
	providerID = NormalizeProvider(providerID)
	return s.providers[providerID].Status(ctx)
}

func (s *Service) Generate(
	ctx context.Context,
	providerID, model, prompt string,
	onDelta func(string),
) (GenerationResult, error) {
	providerID = NormalizeProvider(providerID)
	model = NormalizeModel(providerID, model)
	selected := s.providers[providerID]
	if selected == nil {
		return GenerationResult{}, fmt.Errorf("unsupported AI provider %q", providerID)
	}
	return selected.Generate(ctx, model, prompt, onDelta)
}

func GenerationModel(providerID, model string) string {
	providerID = NormalizeProvider(providerID)
	return providerID + "/" + NormalizeModel(providerID, model)
}

func providerModels(providerID string) []ModelOption {
	if providerID == ProviderOpenAI {
		return []ModelOption{{
			ID:          DefaultOpenAIModel,
			Name:        "Codex default",
			Description: "Tracks the default model selected by your installed Codex CLI.",
		}}
	}
	return []ModelOption{
		{ID: "opus", Name: "Opus", Description: "Highest-quality Claude model alias."},
		{ID: "sonnet", Name: "Sonnet", Description: "Balanced Claude model alias."},
		{ID: "haiku", Name: "Haiku", Description: "Fastest Claude model alias."},
	}
}

func actionableGenerationError(providerID string, err error) error {
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(err.Error())
	lower := strings.ToLower(message)
	authFailure := strings.Contains(lower, "authentication") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "not logged in") ||
		strings.Contains(lower, "login required") ||
		strings.Contains(lower, "please login") ||
		strings.Contains(lower, "please run /login") ||
		strings.Contains(lower, "invalid api key") ||
		strings.Contains(lower, "token expired") ||
		strings.Contains(lower, "status 401") ||
		strings.Contains(lower, "status code 401")
	if !authFailure {
		return err
	}
	if providerID == ProviderOpenAI {
		return fmt.Errorf("OpenAI authentication failed. Run `codex login` and choose Sign in with ChatGPT, then retry. Details: %s", truncate(message, 500))
	}
	return fmt.Errorf("Claude authentication failed. Run `claude auth login`, then retry. Details: %s", truncate(message, 500))
}
