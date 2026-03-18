package llm

import (
	"fmt"
	"strings"
)

// Backend identifies which LLM API protocol to use.
const (
	BackendOpenAIChat      = "openai-chat"
	BackendOpenAIResponses = "openai-responses"
	BackendAnthropic       = "anthropic"
)

// ProviderConfig holds all configuration needed to create a provider.
type ProviderConfig struct {
	Backend      string // "openai-chat", "openai-responses", "anthropic", or "" for auto-detect
	APIKey       string
	BaseURL      string
	Model        string
	UseWebSocket bool // only relevant for openai-responses backend
}

// NewProvider creates the appropriate provider based on configuration.
// If Backend is empty, it auto-detects from the model name and base URL.
func NewProvider(cfg ProviderConfig) (Provider, error) {
	backend := cfg.Backend
	if backend == "" {
		backend = DetectBackend(cfg.Model, cfg.BaseURL)
	}

	switch backend {
	case BackendAnthropic:
		if cfg.BaseURL == "" {
			cfg.BaseURL = defaultAnthropicBaseURL
		}
		return NewAnthropicProvider(cfg.APIKey, cfg.BaseURL), nil

	case BackendOpenAIResponses:
		if cfg.BaseURL == "" {
			cfg.BaseURL = defaultOpenAIBaseURL
		}
		if cfg.UseWebSocket {
			return NewOpenAIResponsesWSProvider(cfg.APIKey, cfg.BaseURL), nil
		}
		return NewOpenAIResponsesProvider(cfg.APIKey, cfg.BaseURL), nil

	case BackendOpenAIChat:
		return NewOpenAIProvider(cfg.APIKey, cfg.BaseURL), nil

	default:
		return nil, fmt.Errorf("unknown LLM backend: %q", backend)
	}
}

// DetectBackend infers the backend from model name and base URL.
//
// Only selects Anthropic when the base URL points to Anthropic's API (or is empty
// and the model starts with "claude-"). If a custom base URL is set (e.g. OpenRouter,
// Bedrock, or a local proxy), we always default to OpenAI Chat Completions since
// that's the universal compatibility format these proxies expose.
func DetectBackend(model, baseURL string) string {
	// Explicit Anthropic base URL → use Anthropic API
	if strings.Contains(baseURL, "anthropic.com") {
		return BackendAnthropic
	}

	// Custom base URL set → the user is routing through a proxy (OpenRouter, Bedrock, etc.)
	// These proxies speak OpenAI Chat Completions format regardless of the underlying model.
	if baseURL != "" {
		return BackendOpenAIChat
	}

	// No base URL + claude model → direct Anthropic API
	if strings.HasPrefix(strings.ToLower(model), "claude-") {
		return BackendAnthropic
	}

	return BackendOpenAIChat
}

// ProviderInfo returns a human-readable description of the provider configuration
// for display in the welcome banner (e.g. "anthropic · stateless" or "openai-responses · websocket · stateful").
func (cfg ProviderConfig) ProviderInfo() string {
	backend := cfg.Backend
	if backend == "" {
		backend = DetectBackend(cfg.Model, cfg.BaseURL)
	}

	switch backend {
	case BackendAnthropic:
		return "anthropic"
	case BackendOpenAIResponses:
		if cfg.UseWebSocket {
			return "openai-responses [websocket]"
		}
		return "openai-responses"
	case BackendOpenAIChat:
		return "openai-chat"
	default:
		return backend
	}
}
