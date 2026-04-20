package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sazid/bitcode/internal"
)

type WebSearchInput struct {
	Query          string   `json:"query"`
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	BlockedDomains []string `json:"blocked_domains,omitempty"`
}

type WebSearchTool struct{}

var _ Tool = (*WebSearchTool)(nil)

func (w *WebSearchTool) Name() string {
	return "WebSearch"
}

func (w *WebSearchTool) Description() string {
	return `Search the web for up-to-date external information.

When to use this:
- Use WebSearch when the answer depends on current facts, recent releases, or online documentation outside the repository.
- Prefer this over guessing when the model may be past its knowledge cutoff.
- Use domain filters when you want to include or exclude specific websites.

Important:
- Requires the BRAVE_API_KEY environment variable.
- Returns titles, URLs, and descriptions for matching results.
- After using search results in your answer, include a Sources section with the relevant URLs.

Parameters:
- query (required): Search query.
- allowed_domains (optional): Restrict results to these domains.
- blocked_domains (optional): Exclude these domains.`
}

func (w *WebSearchTool) ParametersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query to use",
				"minLength":   2,
			},
			"allowed_domains": map[string]any{
				"type":        "array",
				"description": "Only include search results from these domains",
				"items": map[string]any{
					"type": "string",
				},
			},
			"blocked_domains": map[string]any{
				"type":        "array",
				"description": "Never include search results from these domains",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		"required": []string{"query"},
	}
}

func (w *WebSearchTool) Execute(input json.RawMessage, eventsCh chan<- internal.Event) (ToolResult, error) {
	var params WebSearchInput
	if err := json.Unmarshal(input, &params); err != nil {
		return ToolResult{}, fmt.Errorf("invalid input: %w", err)
	}

	if len(params.Query) < 2 {
		return ToolResult{}, fmt.Errorf("query must be at least 2 characters")
	}

	apiKey := os.Getenv("BRAVE_API_KEY")
	if apiKey == "" {
		return ToolResult{}, fmt.Errorf("BRAVE_API_KEY environment variable is not set. Get a free API key at https://brave.com/search/api/")
	}

	// Build Brave Search API URL
	searchURL := "https://api.search.brave.com/res/v1/web/search"
	q := url.Values{}
	q.Set("q", params.Query)
	q.Set("count", "10")
	searchURL += "?" + q.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("X-Subscription-Token", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ToolResult{}, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return ToolResult{}, fmt.Errorf("Brave Search API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response
	var searchResp braveSearchResponse
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return ToolResult{}, fmt.Errorf("failed to parse search results: %w", err)
	}

	// Build allowed/blocked domain sets for filtering
	allowedSet := make(map[string]bool)
	for _, d := range params.AllowedDomains {
		allowedSet[strings.ToLower(d)] = true
	}
	blockedSet := make(map[string]bool)
	for _, d := range params.BlockedDomains {
		blockedSet[strings.ToLower(d)] = true
	}

	// Format results
	var sb strings.Builder
	var previewLines []string
	resultCount := 0

	for _, result := range searchResp.Web.Results {
		// Apply domain filtering
		domain := extractDomain(result.URL)
		if len(allowedSet) > 0 && !allowedSet[domain] {
			continue
		}
		if blockedSet[domain] {
			continue
		}

		resultCount++
		fmt.Fprintf(&sb, "## %d. %s\n", resultCount, result.Title)
		fmt.Fprintf(&sb, "**URL:** %s\n", result.URL)
		if result.Description != "" {
			fmt.Fprintf(&sb, "%s\n", result.Description)
		}
		sb.WriteString("\n")

		if len(previewLines) < 5 {
			previewLines = append(previewLines, fmt.Sprintf("%d. %s", resultCount, result.Title))
		}
	}

	if resultCount == 0 {
		sb.WriteString("No results found.")
		previewLines = append(previewLines, "No results found")
	}

	eventsCh <- internal.Event{
		Name:        w.Name(),
		Args:        []string{params.Query},
		Message:     fmt.Sprintf("Found %d results", resultCount),
		Preview:     previewLines,
		PreviewType: internal.PreviewPlain,
		IsError:     resultCount == 0,
	}

	return ToolResult{
		Content: sb.String(),
	}, nil
}

// extractDomain extracts the domain from a URL string.
func extractDomain(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	// Strip "www." prefix for matching
	host = strings.TrimPrefix(host, "www.")
	return host
}

// Brave Search API response types

type braveSearchResponse struct {
	Web braveWebResults `json:"web"`
}

type braveWebResults struct {
	Results []braveWebResult `json:"results"`
}

type braveWebResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}
