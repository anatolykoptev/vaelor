package codegraph

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/anatolykoptev/go-code/internal/llmiface"
	"github.com/anatolykoptev/go-code/internal/prompts"
)

// reCodeBlock matches a fenced code block, optionally with a language tag.
var reCodeBlock = regexp.MustCompile("(?s)```(?:cypher|)\\n(.+?)\\n```")

// cypherSystemPrompt returns the Cypher generation system prompt with the full
// graph schema injected.
func cypherSystemPrompt() string {
	return fmt.Sprintf(prompts.SystemPromptGenerateCypher, GraphSchemaText())
}

// GenerateCypher asks the LLM to produce a read-only Cypher query for the given
// natural-language question. It extracts the query from any markdown code block
// returned by the model and rejects queries containing write operations.
func GenerateCypher(ctx context.Context, client llmiface.Completer, query string) (string, error) {
	raw, err := client.Complete(ctx, cypherSystemPrompt(), query)
	if err != nil {
		return "", fmt.Errorf("llm completion: %w", err)
	}

	cypher := extractCypher(raw)

	if !isReadOnly(cypher) {
		return "", fmt.Errorf("generated Cypher contains write operations: %q", cypher)
	}

	return cypher, nil
}

// vleNullHint describes the AGE VLE NULL-start-node pitfall so the retry
// prompt nudges the model toward a guarded rewrite instead of random variants.
const vleNullHint = "\n\nHint: the failing query likely starts a variable-length path ([:REL*1..N]) from a node that was bound by OPTIONAL MATCH and may be NULL. " +
	"AGE requires the start node to be non-NULL. Insert `WITH startNode WHERE startNode IS NOT NULL` before the VLE, or turn the first OPTIONAL MATCH into a required MATCH."

// GenerateCypherWithRetry retries Cypher generation with the previous error
// attached to the prompt so the model can self-correct.
func GenerateCypherWithRetry(ctx context.Context, client llmiface.Completer, query, firstErr string) (string, error) {
	hint := ""
	if strings.Contains(firstErr, "match_vle_terminal_edge") {
		hint = vleNullHint
	}
	retryPrompt := fmt.Sprintf(
		"%s\n\nPrevious attempt failed with error: %s%s\nPlease fix the query.",
		query, firstErr, hint,
	)

	raw, err := client.Complete(ctx, cypherSystemPrompt(), retryPrompt)
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
