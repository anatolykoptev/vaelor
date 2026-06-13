package embeddings

import "context"

// codeRankEmbedModel is the canonical name for CodeRankEmbed as registered on
// the embed-server. Any model whose name equals this constant requires the
// retrieval-prefix asymmetry: queries are prefixed, indexed documents are not.
const codeRankEmbedModel = "code-rank-embed"

// codeRankQueryPrefix is prepended to query strings (retrieval use case only)
// when the active model is CodeRankEmbed. Indexed document text is NEVER
// prefixed — the asymmetry is deliberate and required by the model spec.
//
// References:
//   - https://huggingface.co/nomic-ai/CodeRankEmbed
//   - "Represent this query for searching relevant code: " is the exact prefix
//     from the CodeRankEmbed paper/HF README. Do NOT change the wording.
const codeRankQueryPrefix = "Represent this query for searching relevant code: "

// codeRankQueryClient wraps an embed.EmbedQuery-capable client and prepends the
// required retrieval prefix for CodeRankEmbed before forwarding to the backend.
// Document embeddings (Embed calls) pass through without modification.
//
// Design: the prefix is applied once, at the outermost query boundary (callers
// of EmbedQuery), not inside the embed.Client chain. This keeps the go-kit
// embed.Client oblivious to model-specific semantics and localises the
// asymmetry decision in this package.
type codeRankQueryClient struct {
	inner    embedQueryer
	modelTag string // "code-rank-embed" or similar — stored for observability
}

// QueryEmbedder is the subset of embed.Client that query-path callers need.
// Exported so cmd/go-code can use it as the SemanticDeps.QueryClient field type.
// Satisfied by *embed.Client and by *codeRankQueryClient.
type QueryEmbedder interface {
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

// embedQueryer is the unexported alias used internally and in tests.
type embedQueryer = QueryEmbedder

// NewQueryClient wraps client so that EmbedQuery calls apply the model-correct
// retrieval prefix. When model != codeRankEmbedModel the client is returned
// unwrapped (zero-overhead, identical to prior behaviour).
//
// Document embedding (Embed calls on the returned *embed.Client) is never
// affected — this wrapper intercepts ONLY EmbedQuery.
func NewQueryClient(client QueryEmbedder, model string) QueryEmbedder {
	return newQueryClient(client, model)
}

// newQueryClient is the unexported form used in tests within this package.
func newQueryClient(client embedQueryer, model string) embedQueryer {
	if model != codeRankEmbedModel {
		return client
	}
	return &codeRankQueryClient{inner: client, modelTag: model}
}

// EmbedQuery prepends the CodeRankEmbed retrieval prefix before forwarding.
// Documents indexed via embed.Client.Embed are never routed here — this method
// is query-path only.
func (c *codeRankQueryClient) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return c.inner.EmbedQuery(ctx, codeRankQueryPrefix+text)
}
