package analyze

import "testing"

func TestDeps_HasLearnings(t *testing.T) {
	var d Deps
	_ = d.Learnings // must compile — type *learnings.Store
}
