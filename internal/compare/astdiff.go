package compare

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/kotlin"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	swift "github.com/smacker/go-tree-sitter/swift"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
	"github.com/smacker/gum"
)

// maxDiffChanges is the maximum number of human-readable change descriptions to include.
const maxDiffChanges = 5

// maxDiffBodyLen is the maximum body length (bytes) to attempt AST diffing on.
// Bodies larger than this are skipped to avoid excessive computation.
const maxDiffBodyLen = 10000

// DiffSummary holds the result of a structural AST diff between two symbol bodies.
type DiffSummary struct {
	// TotalChanges is the total number of edit actions in the diff script.
	TotalChanges int `json:"totalChanges"`

	// Inserts is the count of insert (single node) and insert-tree actions.
	Inserts int `json:"inserts"`

	// Deletes is the count of delete (single node) and delete-tree actions.
	Deletes int `json:"deletes"`

	// Updates is the count of update (value change) actions.
	Updates int `json:"updates"`

	// Moves is the count of move (reorder) actions.
	Moves int `json:"moves"`

	// Changes is a list of human-readable change descriptions (max maxDiffChanges).
	Changes []string `json:"changes,omitempty"`
}

// ComputeASTDiff parses two symbol bodies using tree-sitter for the given language,
// computes a GumTree edit script, and returns a DiffSummary.
// Returns nil if either body is empty, the language is unsupported, or bodies are too large.
func ComputeASTDiff(bodyA, bodyB, language string) *DiffSummary {
	if bodyA == "" || bodyB == "" {
		return nil
	}
	if len(bodyA) > maxDiffBodyLen || len(bodyB) > maxDiffBodyLen {
		return nil
	}

	lang := lookupLanguage(language)
	if lang == nil {
		return nil
	}

	srcA := []byte(bodyA)
	srcB := []byte(bodyB)

	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(lang)

	ctx := context.Background()

	treeA, err := parser.ParseCtx(ctx, nil, srcA)
	if err != nil || treeA == nil {
		return nil
	}
	defer treeA.Close()
	treeB, err := parser.ParseCtx(ctx, nil, srcB)
	if err != nil || treeB == nil {
		return nil
	}
	defer treeB.Close()

	gtA := ToGumTree(treeA.RootNode(), srcA)
	gtB := ToGumTree(treeB.RootNode(), srcB)

	mappings := gum.Match(gtA, gtB)
	actions := gum.Patch(gtA, gtB, mappings)

	summary := &DiffSummary{
		TotalChanges: len(actions),
	}

	for _, a := range actions {
		switch a.Type {
		case gum.Insert, gum.InsertTree:
			summary.Inserts++
		case gum.Delete, gum.DeleteTree:
			summary.Deletes++
		case gum.Update:
			summary.Updates++
		case gum.Move:
			summary.Moves++
		}
	}

	summary.Changes = summarizeActions(actions, language)

	return summary
}

// lookupLanguage maps a language name to its tree-sitter *sitter.Language.
// Returns nil for unsupported languages.
func lookupLanguage(language string) *sitter.Language {
	switch strings.ToLower(language) {
	case "go", "golang":
		return golang.GetLanguage()
	case "python":
		return python.GetLanguage()
	case "javascript", "typescript":
		return typescript.GetLanguage()
	case "rust":
		return rust.GetLanguage()
	case "java":
		return java.GetLanguage()
	case "kotlin":
		return kotlin.GetLanguage()
	case "swift":
		return swift.GetLanguage()
	case "c":
		return c.GetLanguage()
	case "cpp", "c++":
		return cpp.GetLanguage()
	case "ruby":
		return ruby.GetLanguage()
	case "csharp", "c#":
		return csharp.GetLanguage()
	default:
		return nil
	}
}

// summarizeActions produces human-readable descriptions of the most important
// edit actions, capped at maxDiffChanges entries.
func summarizeActions(actions []*gum.Action, language string) []string {
	if len(actions) == 0 {
		return nil
	}

	var result []string
	for _, a := range actions {
		if len(result) >= maxDiffChanges {
			break
		}
		desc := describeAction(a)
		if desc != "" {
			result = append(result, desc)
		}
	}

	return result
}

// describeAction returns a human-readable description of a single gum.Action.
func describeAction(a *gum.Action) string {
	switch a.Type {
	case gum.Insert:
		return "insert " + nodeDescription(a.Node)
	case gum.InsertTree:
		return "insert tree " + nodeDescription(a.Node)
	case gum.Delete:
		return "delete " + nodeDescription(a.Node)
	case gum.DeleteTree:
		return "delete tree " + nodeDescription(a.Node)
	case gum.Update:
		return fmt.Sprintf("update %s to %s", nodeDescription(a.Node), truncateValue(a.Value))
	case gum.Move:
		return "move " + nodeDescription(a.Node)
	default:
		return ""
	}
}

// nodeDescription returns a human-readable label for a gum.Tree node.
func nodeDescription(t *gum.Tree) string {
	if t == nil {
		return "<nil>"
	}
	if t.Value != "" {
		return fmt.Sprintf("%s(%s)", t.Type, truncateValue(t.Value))
	}
	return t.Type
}

// truncateValue shortens a string to at most 40 characters, appending "..." if truncated.
func truncateValue(s string) string {
	const maxLen = 40
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
