package compare

import "github.com/anatolykoptev/go-code/internal/artifactfilter"

// IsCompiledArtifact returns true when filePath looks like a build output
// that should be excluded from coupling and other source-level analyses.
//
// The implementation lives in internal/artifactfilter (stdlib-only leaf) so
// that packages like internal/federate can import just that leaf without
// pulling in compare's heavy transitive closure.
func IsCompiledArtifact(filePath string) bool {
	return artifactfilter.IsCompiledArtifact(filePath)
}
