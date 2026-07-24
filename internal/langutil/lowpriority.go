package langutil

import "strings"

// generatedSuffixes covers obvious generated-file suffixes (Zoekt
// lowPriorityFilePenalty territory). These are mechanically produced, not
// hand-written, so a BM25F match there is a weaker relevance signal.
var generatedSuffixes = []string{
	".pb.go",        // protobuf-generated Go
	".pb.gw.go",     // protobuf gRPC gateway generated Go
	"_generated.go", // explicit generated Go
	"_generated.py", // explicit generated Python
	".g.dart",       // Dart codegen (freezed/json_serializable etc.)
}

// generatedInfixes covers generated-file infix markers that are not suffixes.
var generatedInfixes = []string{
	"_generated.", // e.g. foo_generated.rs, bar_generated.ts
}

// IsGeneratedFile reports whether path looks like a mechanically generated
// file (protobuf, codegen, explicit "_generated" markers). It is intentionally
// separate from IsTestFile so the many IsTestFile call-sites (graph indexing,
// dead-code, review delta, etc.) keep their original "test file" semantics.
func IsGeneratedFile(path string) bool {
	lower := strings.ToLower(path)
	for _, suf := range generatedSuffixes {
		if strings.HasSuffix(lower, suf) {
			return true
		}
	}
	for _, inf := range generatedInfixes {
		if strings.Contains(lower, inf) {
			return true
		}
	}
	return false
}

// IsLowPriorityFile reports whether path is a test OR generated file — the set
// Zoekt demotes via lowPriorityFilePenalty (index/score.go). It composes the
// existing IsTestFile predicate with IsGeneratedFile so the BM25F scorer can
// apply a single penalty gate without hardcoding a file list.
func IsLowPriorityFile(path string) bool {
	return IsTestFile(path) || IsGeneratedFile(path)
}
