package codegraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-kit/llm"
	"github.com/anatolykoptev/go-code/internal/prompts"
)

// Classification holds the result of classifying a natural-language query.
type Classification struct {
	Template string            `json:"template"`
	Params   map[string]string `json:"params"`
}

// classifierSystemPrompt builds the full system prompt for query classification,
// injecting the graph schema and the available template list.
func classifierSystemPrompt() string {
	return fmt.Sprintf(prompts.SystemPromptClassifyGraphQuery, GraphSchemaText(), TemplateList())
}

// Classify sends a natural-language query to the LLM and returns a Classification.
// On JSON parse failure it returns a freeform fallback rather than an error.
func Classify(ctx context.Context, client llm.Completer, query string) (*Classification, error) {
	systemPrompt := classifierSystemPrompt()
	raw, err := client.Complete(ctx, systemPrompt, query)
	if err != nil {
		return nil, fmt.Errorf("llm classify: %w", err)
	}

	c, err := parseClassification(raw)
	if err != nil {
		// Freeform fallback: LLM gave an unparseable response.
		return &Classification{Template: "freeform", Params: map[string]string{}}, nil
	}
	return c, nil
}

// parseClassification parses the LLM JSON response into a Classification.
// It strips markdown code fences, validates the template field, and substitutes
// unknown template IDs with "freeform".
func parseClassification(raw string) (*Classification, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, errors.New("empty classification response")
	}

	// Strip markdown code fences: ```json ... ``` or ``` ... ```.
	if strings.HasPrefix(s, "```") {
		// Find the end of the opening fence line.
		end := strings.Index(s, "\n")
		if end == -1 {
			return nil, errors.New("malformed markdown fence: no newline found")
		}
		s = s[end+1:]
		// Remove closing fence.
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	}

	var c Classification
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return nil, fmt.Errorf("unmarshal classification: %w", err)
	}

	if c.Template == "" {
		return nil, errors.New("classification missing template field")
	}

	// Unknown template ID → freeform fallback.
	if c.Template != "freeform" && GetTemplate(c.Template) == nil {
		c.Template = "freeform"
	}

	// Nil params → empty map.
	if c.Params == nil {
		c.Params = map[string]string{}
	}

	return &c, nil
}
