// Package llm provides an OpenAI-compatible LLM client for go-code.
//
// It targets CLIProxyAPI at :8317 (configured via LLM_URL env var) which
// routes requests across Gemini OAuth accounts with quota switching.
// Supports structured JSON output extraction and streaming (future).
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	defaultModel       = "gemini-2.5-flash"
	defaultMaxTokens   = 16384
	defaultTemperature = 0.1
	defaultTimeout     = 90 * time.Second

	completionsPath = "/chat/completions"
)

// Client is an OpenAI-compatible LLM client.
type Client struct {
	baseURL    string
	apiKey     string
	model      string
	maxTokens  int
	httpClient *http.Client
}

// Config holds LLM client configuration.
type Config struct {
	// BaseURL is the OpenAI-compatible base URL (e.g. http://127.0.0.1:8317/v1).
	BaseURL string

	// APIKey is the API key for authentication.
	APIKey string //nolint:gosec // not a hardcoded secret — loaded from env

	// Model is the model name to use.
	Model string

	// MaxTokens limits the response length.
	MaxTokens int
}

// Message is a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CompletionRequest is the request body for a chat completion.
type CompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
}

// CompletionResponse is the response from a chat completion.
type CompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// NewClient creates a new LLM client with the given configuration.
func NewClient(cfg Config) *Client {
	model := cfg.Model
	if model == "" {
		model = defaultModel
	}

	maxTokens := cfg.MaxTokens
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
	}

	return &Client{
		baseURL:   strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:    cfg.APIKey,
		model:     model,
		maxTokens: maxTokens,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// Complete sends a chat completion request and returns the response text.
func (c *Client) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	reqBody := CompletionRequest{
		Model:       c.model,
		Messages:    messages,
		MaxTokens:   c.maxTokens,
		Temperature: defaultTemperature,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + completionsPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // URL comes from trusted config (LLM_URL env var)
	if err != nil {
		return "", fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm returned %d", resp.StatusCode)
	}

	var completion CompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if len(completion.Choices) == 0 {
		return "", errors.New("empty choices in llm response")
	}

	return completion.Choices[0].Message.Content, nil
}

// SystemPromptRepoAnalysis is the system prompt for repository analysis queries.
const SystemPromptRepoAnalysis = `You are a senior software engineer analyzing a code repository.
You have been provided with the repository's file tree, key source files, and parsed symbol information.
Answer the user's question about the codebase accurately and concisely.
Focus on architecture, design decisions, and implementation patterns.
Use code examples from the provided context when relevant.
If you cannot answer from the provided context, say so clearly.`

// SystemPromptCodeCompare is the system prompt for code comparison queries.
const SystemPromptCodeCompare = `You are a lead software engineer conducting a comparative code review of two repositories.
Your task is to find the BETTER solution — more modern, more optimized, more scalable, higher quality.

You receive: matched symbol pairs (side-by-side code), coverage gaps, and computed metrics.

Respond with ONLY a JSON object (no markdown, no explanation outside JSON):

{
  "quality": [
    {
      "aspect": "error handling",
      "winner": "repo_a" or "repo_b",
      "reason": "concise explanation with evidence",
      "snippetA": "relevant code from repo A",
      "snippetB": "relevant code from repo B"
    }
  ],
  "gaps": [
    {
      "missingIn": "repo_a" or "repo_b",
      "feature": "what is missing",
      "locationB": "file path where it exists",
      "importance": "high" or "medium" or "low"
    }
  ],
  "architecture": [
    {
      "insight": "pattern or architectural decision worth adopting",
      "source": "repo_a" or "repo_b",
      "example": "specific file or function",
      "benefit": "why this improves the codebase"
    }
  ],
  "recommendations": [
    "Actionable recommendation 1",
    "Actionable recommendation 2"
  ]
}

Focus on:
1. Implementation quality — cleaner, more optimized, more modern approach
2. Missing functionality — features one repo has that the other lacks
3. Architecture — package structure, separation of concerns, extensibility, testability
4. Concrete recommendations — specific actions to improve the weaker repo`

// SystemPromptDepGraph is the system prompt for dependency graph analysis.
const SystemPromptDepGraph = `You are a senior software engineer analyzing a dependency graph.
Based on the provided import/dependency data, describe:
1. The overall layering and module structure
2. Any circular dependencies or problematic coupling
3. Hotspot packages (many dependents)
4. Suggestions for improving the dependency structure
Be concise and actionable.`

// SystemPromptOverview is the system prompt for high-level repository analysis.
const SystemPromptOverview = `You are a senior software engineer providing a high-level overview of a code repository.
Focus on: public API surface, key architectural components, package organization, and design patterns.
Be concise — summarize the architecture, don't enumerate every function.
Use the provided symbol signatures and file tree to identify the main modules and their responsibilities.`

// SystemPromptDeep is the system prompt for deep repository analysis.
const SystemPromptDeep = `You are a senior software engineer doing deep analysis of a code repository.
Focus on: implementation details, algorithms, error handling, edge cases, and performance characteristics.
Reference specific functions, line numbers, and code patterns.
Explain how components interact at the implementation level, not just the interface level.`

// SystemPromptCallTrace is the system prompt for call chain narrative generation.
const SystemPromptCallTrace = `You are a senior software engineer explaining an execution path through a codebase.
You receive a call chain trace (JSON tree of function calls).

Explain step-by-step what happens when the entry function is called:
1. What each function does (based on its name and signature)
2. Key decision points and error handling paths
3. External calls that leave the codebase (stdlib, third-party)
4. Cycles or recursive patterns if present

Be concise and focus on the flow, not line-by-line details.
Format as a numbered walkthrough.`

// SystemPromptForDepth returns the appropriate system prompt for the given analysis depth.
// Depth values match analyze.Depth* constants but are repeated here
// to avoid a circular import between llm → analyze.
func SystemPromptForDepth(depth string) string {
	switch depth {
	case "overview":
		return SystemPromptOverview
	case "deep":
		return SystemPromptDeep
	default:
		return SystemPromptRepoAnalysis
	}
}
