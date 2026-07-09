package analyze

import "testing"

func TestDeps_HasLearnings(t *testing.T) {
	t.Parallel()
	var d Deps
	_ = d.Learnings // must compile — type *learnings.Store
}
