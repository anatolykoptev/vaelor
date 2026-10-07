package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/impact"
)

func TestDirectCallersTruncationNote_NamesOmittedAndSaysTheyAreCounted(t *testing.T) {
	t.Parallel()
	var omitted []impact.AffectedSymbol
	for i := range 13 {
		omitted = append(omitted, impact.AffectedSymbol{Name: fmt.Sprintf("Caller%02d", i)})
	}
	note := directCallersTruncationNote(100, 113, omitted, "ParseFile")

	for _, want := range []string{
		"lists 100 of 113", "13 omitted", "Caller00", "Caller09", "+3 more",
		"still counted in total_affected", `call_trace symbol="ParseFile"`,
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "Caller10") {
		t.Errorf("note must cap spelled-out names at %d:\n%s", maxOmittedNamesInNote, note)
	}
}
