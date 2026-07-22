package argnorm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const toolsCallMethod = "tools/call"

// maxDidYouMean is the number of suggestions included in an unknown-tool error.
const maxDidYouMean = 2

// Middleware returns an mcp.Middleware that normalizes tool-call arguments
// before the framework's strict schema validation runs. It must be installed as
// the FIRST receiving middleware so downstream metrics/tracing observe the
// resolved tool name and normalized args.
//
// Behaviour:
//  1. Tool-name alias: github_repo_search → github_code_search (issue #570).
//  2. Unknown tool: short did-you-mean error instead of a bare -32602.
//  3. Argument aliases: limit→max_results/top_k, insights→repo (issue #568).
//  4. Tolerant reader: strip unknown properties, append a note to success
//     responses naming the ignored params and the supported set (issue #568).
//
// reg may be nil, in which case Default() is used.
func Middleware(reg *Registry) mcp.Middleware {
	if reg == nil {
		reg = Default()
	}
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != toolsCallMethod {
				return next(ctx, method, req)
			}
			params, ok := req.GetParams().(*mcp.CallToolParamsRaw)
			if !ok || params == nil {
				return next(ctx, method, req)
			}

			// 1. Tool-name alias rewrite.
			if canonical, aliased := ResolveToolName(params.Name); aliased {
				params.Name = canonical
			}

			// 2. Unknown tool → did-you-mean error (before next, so the SDK's
			//    bare "unknown tool" -32602 never reaches the client).
			if !reg.Has(params.Name) {
				return didYouMeanResult(params.Name, reg.Names()), nil
			}

			// 3+4. Argument normalization (aliases + strip unknowns).
			accepted, open, _ := reg.Accepted(params.Name)
			if !open {
				newArgs, nres, perr := normalizeRawMessage(params.Name, params.Arguments, accepted)
				if perr == nil {
					params.Arguments = newArgs
				}
				// Stash the note for post-next appending via context-free
				// closure: we append after next returns so the note rides on
				// the actual response.
				result, err := next(ctx, method, req)
				if err != nil {
					return result, err
				}
				if nres.Stripped == nil || len(nres.Stripped) == 0 {
					return result, err
				}
				note := nres.Note(accepted)
				for _, p := range nres.Stripped {
					if hint := StrippedHint(params.Name, p); hint != "" {
						note += " — " + hint
					}
				}
				return appendNote(result, note), nil
			}

			return next(ctx, method, req)
		}
	}
}

// didYouMeanResult builds the short unknown-tool error result with closest
// matches. Format (issue #570):
//
//	unknown tool "github_repo_search" — did you mean "github_code_search"?
func didYouMeanResult(name string, candidates []string) *mcp.CallToolResult {
	suggestions := DidYouMean(name, candidates, maxDidYouMean)
	msg := fmt.Sprintf("unknown tool %q", name)
	if len(suggestions) > 0 {
		quoted := make([]string, len(suggestions))
		for i, s := range suggestions {
			quoted[i] = `"` + s + `"`
		}
		msg += " — did you mean " + joinOr(quoted) + "?"
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}

// joinOr joins a list as English prose: "a", "a or b", "a, b or c".
func joinOr(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " or " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
	}
}

// appendNote adds a one-line note as an extra TextContent block on a
// CallToolResult. Non-CallToolResult results are returned unchanged. The note
// is appended only to non-error results so tool-error messages stay clean.
func appendNote(result mcp.Result, note string) mcp.Result {
	if note == "" {
		return result
	}
	cr, ok := result.(*mcp.CallToolResult)
	if !ok || cr == nil {
		return result
	}
	if cr.IsError {
		return result
	}
	// Defensive copy so we never mutate a result owned by another layer.
	out := *cr
	out.Content = append([]mcp.Content{}, cr.Content...)
	out.Content = append(out.Content, &mcp.TextContent{Text: note})
	return &out
}

// MarshalArgs is a small helper exported for tests: it marshals a map to a
// json.RawMessage, panicking on error (test-only, maps are always serializable).
func MarshalArgs(m map[string]any) json.RawMessage {
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}
