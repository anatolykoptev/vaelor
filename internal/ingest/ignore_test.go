package ingest

import (
	"sort"
	"testing"
)

func TestIgnoredDirNames_ReturnsSorted(t *testing.T) {
	names := IgnoredDirNames()
	if len(names) == 0 {
		t.Fatal("expected non-empty list from IgnoredDirNames")
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("IgnoredDirNames() is not sorted: %v", names)
	}
}

func TestIgnoredDirNames_ContainsKnownEntries(t *testing.T) {
	names := IgnoredDirNames()
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	for _, want := range []string{".claude", "vendor", "testdata", "node_modules", "migrations"} {
		if !set[want] {
			t.Errorf("IgnoredDirNames() missing %q", want)
		}
	}
}
