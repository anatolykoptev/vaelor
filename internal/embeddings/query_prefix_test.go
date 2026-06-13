package embeddings

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureEmbedQueryer records the last text passed to EmbedQuery.
type captureEmbedQueryer struct {
	lastText string
}

func (c *captureEmbedQueryer) EmbedQuery(_ context.Context, text string) ([]float32, error) {
	c.lastText = text
	return []float32{0.1, 0.2, 0.3}, nil
}

// TestQueryPrefix_CodeRankEmbed_Query: EmbedQuery for code-rank-embed MUST
// prepend the retrieval prefix. Passes when behaviour is present; fails RED
// if the codeRankQueryClient wrapper is removed or the model check is wrong.
func TestQueryPrefix_CodeRankEmbed_Query(t *testing.T) {
	cap := &captureEmbedQueryer{}
	client := newQueryClient(cap, codeRankEmbedModel)

	_, err := client.EmbedQuery(context.Background(), "find auth handler")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(cap.lastText, codeRankQueryPrefix),
		"query embed for code-rank-embed must have prefix %q; got %q",
		codeRankQueryPrefix, cap.lastText)
	assert.Contains(t, cap.lastText, "find auth handler",
		"original query text must still be present after prefix")
}

// TestQueryPrefix_NonPrefixModel_Query: EmbedQuery for a model that does not
// require a prefix (e.g. jina-code-v2) must pass the text through unchanged.
// This guards the default path — no regression on model swap back.
func TestQueryPrefix_NonPrefixModel_Query(t *testing.T) {
	for _, model := range []string{"jina-code-v2", "multilingual-e5-large", ""} {
		model := model
		t.Run(model, func(t *testing.T) {
			cap := &captureEmbedQueryer{}
			client := newQueryClient(cap, model)

			query := "find auth handler"
			_, err := client.EmbedQuery(context.Background(), query)
			require.NoError(t, err)

			assert.Equal(t, query, cap.lastText,
				"model %q: query text must be unchanged (no prefix)", model)
		})
	}
}

// TestQueryPrefix_DocEmbed_NeverPrefixed: document text (Embed path, not
// EmbedQuery) must NEVER receive the retrieval prefix regardless of model.
// This test verifies the architecture invariant: newQueryClient only wraps
// EmbedQuery; the Embed method on embed.Client is the document path and is
// never routed through codeRankQueryClient.
//
// We assert via the wrapper type: a non-code-rank model returns the original
// client unwrapped (the doc-path Embed goes straight to the backend), and a
// code-rank model returns a wrapper that only intercepts EmbedQuery.
func TestQueryPrefix_DocEmbed_NeverPrefixed(t *testing.T) {
	cap := &captureEmbedQueryer{}

	// Non-prefix model — newQueryClient must return the original (not wrapped).
	plainClient := newQueryClient(cap, "jina-code-v2")
	assert.Same(t, cap, plainClient,
		"non-prefix model: newQueryClient must return the original client unwrapped")

	// Prefix model — newQueryClient must return a wrapper (not the original).
	wrappedClient := newQueryClient(cap, codeRankEmbedModel)
	assert.NotSame(t, cap, wrappedClient,
		"code-rank model: newQueryClient must return a wrapper, not the original")
	_, isCRWrapper := wrappedClient.(*codeRankQueryClient)
	assert.True(t, isCRWrapper,
		"code-rank model: wrapper must be *codeRankQueryClient")
}

// TestQueryPrefix_ModelConst: codeRankEmbedModel must equal the deployed
// model name. Guards against typos that would silently skip prefix injection.
func TestQueryPrefix_ModelConst(t *testing.T) {
	assert.Equal(t, "code-rank-embed", codeRankEmbedModel,
		"codeRankEmbedModel constant must match the deployed model name")
}

// TestQueryPrefix_PrefixConst: codeRankQueryPrefix must be the exact string
// from the CodeRankEmbed spec. Guards against reformatting that would break
// retrieval quality.
func TestQueryPrefix_PrefixConst(t *testing.T) {
	assert.Equal(t, "Represent this query for searching relevant code: ", codeRankQueryPrefix,
		"codeRankQueryPrefix must match the CodeRankEmbed paper spec exactly")
}
