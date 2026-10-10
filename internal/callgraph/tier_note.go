package callgraph

import (
	"fmt"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/polyglot"
	gocodescip "github.com/anatolykoptev/vaelor/internal/scip"
)

// typedSkipReasons explains, per language that has a typed (SCIP) indexer, why
// the graph is still at the tree-sitter tier after the SCIP leg. The go/types
// leg has its own WarmNote, so Go is not repeated here.
//
// It re-derives the reason from the same gates trySCIPResolution applies
// (size cap, indexer registry, binary on PATH, repo trust) instead of having
// that function return it, so the 0-edge outcome — every gate passed, the
// indexer ran, nothing came back — is reported too.
func typedSkipReasons(root string, files []*ingest.File) []string {
	langs := polyglot.DetectedLanguages(files)
	srcFiles := ingest.CountSourceFiles(files)
	var out []string
	for _, lang := range langs {
		cfg, ok := gocodescip.DetectIndexer(lang)
		if !ok || lang == "go" {
			continue
		}
		var why string
		switch {
		case srcFiles > maxSCIPSourceFiles:
			why = fmt.Sprintf("repo has %d source files, over the %d-file typed-indexing cap", srcFiles, maxSCIPSourceFiles)
		case !gocodescip.IndexerAvailable(cfg.Name):
			why = cfg.Name + " is not installed"
		case !gocodescip.IndexerTrusted(lang, root):
			why = cfg.Name + " was not run: it executes repo code and this repo is not an operator-trusted checkout"
		default:
			why = cfg.Name + " ran but returned no call edges"
		}
		out = append(out, lang+": "+why)
	}
	return out
}

// TierNote renders the agent-facing note for a graph that stayed at the
// tree-sitter tier because typed resolution did not run, or "" when nothing
// applies. It exists so an empty or short caller list is never mistaken for
// "no callers": without typed edges, a call through a variable or attribute is
// resolved only where the receiver's class is evident from the source.
func TierNote(cg *CallGraph) string {
	if cg == nil || cg.Tier != "basic" || len(cg.TypedSkipped) == 0 {
		return ""
	}
	return "tier basic: typed call resolution did not run (" + strings.Join(cg.TypedSkipped, "; ") +
		"); callers are matched by name and simple constructor bindings only, so a missing or empty caller list is not proof that nothing calls this symbol"
}
