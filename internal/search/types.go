// Package search provides a SearXNG client and utility functions for
// querying and filtering web search results.
package search

// Result is a search result from SearXNG or other sources.
type Result struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// SearchOpts controls SearXNG search behavior.
type SearchOpts struct {
	Language  string
	TimeRange string
	Engines   string
}
