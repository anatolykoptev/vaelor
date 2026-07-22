package argnorm

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// NormalizeResult is the outcome of normalizing a tool call's arguments.
type NormalizeResult struct {
	// Args is the normalized argument map (unknowns stripped, aliases applied).
	// nil when the input was not a JSON object (caller should pass through
	// unchanged so framework validation reports the real parse error).
	Args map[string]any
	// Stripped lists unknown property names that were removed.
	Stripped []string
	// Aliased lists "src→dst" renames that were applied.
	Aliased []string
}

// Note returns the one-line tolerant-reader note to append to a successful
// response, or "" when nothing was stripped. Format (issue #568):
//
//	note: ignored unknown params ["x"] — supported: [a, b, c]
//
// The supported list is the accepted set, sorted, capped at 12 entries to
// keep the note short; an elision marker is appended when truncated.
func (nr NormalizeResult) Note(accepted map[string]struct{}) string {
	if len(nr.Stripped) == 0 {
		return ""
	}
	stripped := append([]string{}, nr.Stripped...)
	sort.Strings(stripped)
	supported := make([]string, 0, len(accepted))
	for k := range accepted {
		supported = append(supported, k)
	}
	sort.Strings(supported)
	if len(supported) > 12 {
		supported = append(supported[:12], "…")
	}
	return fmt.Sprintf("note: ignored unknown params %s — supported: %s",
		quoteList(stripped), quoteList(supported))
}

func quoteList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = `"` + s + `"`
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// NormalizeArgs applies alias renames and unknown-property stripping to a
// tool's raw argument map. It does NOT mutate the input map — it returns a new
// map. Aliases are applied first (so a renamed-away src is not also reported
// as stripped); then any remaining key not in accepted is stripped.
//
// When accepted is nil (open schema) no stripping is performed and no note
// is produced — open-schema tools accept anything by definition. A non-nil
// but empty accepted map means the tool accepts NO params (closed empty struct,
// #581): all keys are stripped.
func NormalizeArgs(toolName string, raw map[string]any, accepted map[string]struct{}) NormalizeResult {
	if raw == nil {
		return NormalizeResult{}
	}
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		out[k] = v
	}

	res := NormalizeResult{Args: out}

	// Open schema (nil accepted): accept everything, no stripping, no note.
	if accepted == nil {
		return res
	}

	// 1. Apply aliases (src→dst) when dst is accepted and src is not.
	aliases := aliasTargetsFor(toolName, accepted)
	// Deterministic order for stable Aliased reporting.
	srcs := make([]string, 0, len(aliases))
	for src := range aliases {
		srcs = append(srcs, src)
	}
	sort.Strings(srcs)
	for _, src := range srcs {
		if _, hasSrc := out[src]; !hasSrc {
			continue
		}
		dst := aliases[src]
		if _, hasDst := out[dst]; hasDst {
			// Canonical already present — drop the alias copy silently.
			delete(out, src)
			continue
		}
		out[dst] = out[src]
		delete(out, src)
		res.Aliased = append(res.Aliased, src+"→"+dst)
	}

	// 2. Strip unknown properties.
	for k := range out {
		if _, ok := accepted[k]; ok {
			continue
		}
		delete(out, k)
		res.Stripped = append(res.Stripped, k)
	}
	sort.Strings(res.Stripped)
	return res
}

// normalizeRawMessage is the byte-level entry point used by the middleware: it
// parses raw args, normalizes, and re-marshals. Returns the new raw message,
// the NormalizeResult (for note construction), and a parse error when the input
// is not a JSON object (in which case raw is returned unchanged).
func normalizeRawMessage(toolName string, raw json.RawMessage, accepted map[string]struct{}) (json.RawMessage, NormalizeResult, error) {
	if len(raw) == 0 {
		// Empty args → empty object; nothing to normalize.
		return raw, NormalizeResult{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		// Not a JSON object — pass through so framework validation reports the
		// real malformed-input error rather than us masking it.
		return raw, NormalizeResult{}, err
	}
	if m == nil {
		return raw, NormalizeResult{}, nil
	}
	res := NormalizeArgs(toolName, m, accepted)
	if len(res.Aliased) == 0 && len(res.Stripped) == 0 {
		// No changes — return original bytes to keep responses byte-identical.
		return raw, res, nil
	}
	out, err := json.Marshal(res.Args)
	if err != nil {
		return raw, res, err
	}
	return out, res, nil
}
