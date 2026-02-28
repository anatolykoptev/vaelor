package codegraph

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/anatolykoptev/go-code/internal/llm"
)

// reCodeBlock matches a fenced code block, optionally with a language tag.
var reCodeBlock = regexp.MustCompile("(?s)```(?:cypher|)\\n(.+?)\\n```")

// llmCompleter is the interface subset of *llm.Client used by GenerateCypher.
type llmCompleter interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// GenerateCypher asks the LLM to produce a read-only Cypher query for the given
// natural-language question. It extracts the query from any markdown code block
// returned by the model and rejects queries containing write operations.
func GenerateCypher(ctx context.Context, client llmCompleter, query string) (string, error) {
	raw, err := client.Complete(ctx, llm.SystemPromptGenerateCypher, query)
	if err != nil {
		return "", fmt.Errorf("llm completion: %w", err)
	}

	cypher := extractCypher(raw)

	if !isReadOnly(cypher) {
		return "", fmt.Errorf("generated Cypher contains write operations: %q", cypher)
	}

	return cypher, nil
}

// GenerateCypherWithRetry retries Cypher generation with the previous error
// attached to the prompt so the model can self-correct.
func GenerateCypherWithRetry(ctx context.Context, client llmCompleter, query, firstErr string) (string, error) {
	retryPrompt := fmt.Sprintf(
		"%s\n\nPrevious attempt failed with error: %s\nPlease fix the query.",
		query, firstErr,
	)

	raw, err := client.Complete(ctx, llm.SystemPromptGenerateCypher, retryPrompt)
	if err != nil {
		return "", fmt.Errorf("llm retry completion: %w", err)
	}

	cypher := extractCypher(raw)

	if !isReadOnly(cypher) {
		return "", fmt.Errorf("generated Cypher contains write operations: %q", cypher)
	}

	return cypher, nil
}

// extractCypher extracts a Cypher query from an LLM response.
// It tries to match a fenced code block (```cypher or ```) first,
// then falls back to the trimmed raw string.
func extractCypher(raw string) string {
	if m := reCodeBlock.FindStringSubmatch(raw); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(raw)
}
