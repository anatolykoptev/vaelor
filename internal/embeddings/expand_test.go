package embeddings

import (
	"strings"
	"testing"
)

// TestAgeExpandSetupNoLOAD verifies that ageExpandSetup no longer contains a LOAD directive.
// Regression guard: per-connection LOAD was removed in favour of shared_preload_libraries.
// If this test fails, someone re-introduced LOAD — verify postgresql.conf instead.
func TestAgeExpandSetupNoLOAD(t *testing.T) {
	if strings.Contains(strings.ToUpper(ageExpandSetup), "LOAD") {
		t.Errorf("ageExpandSetup must not contain LOAD directive (rely on shared_preload_libraries): %q", ageExpandSetup)
	}
}
