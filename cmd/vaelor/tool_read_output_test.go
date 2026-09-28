package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
)

// #796: the failure this file guards is remote clients receiving a bare
// server-local path for spilled output — the answer simply unreachable.
// Oracle throughout: the original spilled content, reassembled from chunks.

func TestReadOutput_FetchesWholeSpill(t *testing.T) {
	dir := t.TempDir()
	content := "line one\nline two\n<data>payload</data>\n"
	path, ok := saveToFile(content, "code_graph", dir)
	if !ok {
		t.Fatal("fixture is inert: saveToFile failed")
	}

	res := handleReadOutput(ReadOutputInput{Name: filepath.Base(path)}, dir)
	if res.IsError {
		t.Fatalf("unexpected error: %s", textContentOf(t, res))
	}
	got := textContentOf(t, res)
	if !strings.Contains(got, content) {
		t.Fatalf("chunk must contain the spilled content; got %q", got)
	}
	if !strings.HasPrefix(got, filepath.Base(path)+" — chars 0..") {
		t.Fatalf("missing header with name/offset/total; got %q", got)
	}
	if strings.Contains(got, "[truncated:") {
		t.Fatalf("whole file fits — no continuation footer expected; got %q", got)
	}
}

func TestReadOutput_PaginatesAndReassembles(t *testing.T) {
	dir := t.TempDir()
	// 3 windows + a tail — deterministic, no reliance on the window constant.
	unit := "abcdefghij"
	content := strings.Repeat(unit, readOutputWindow/len(unit)*3+1000)
	path, _ := saveToFile(content, "big_tool", dir)
	name := filepath.Base(path)

	var reassembled strings.Builder
	offset := 0
	pages := 0
	for {
		res := handleReadOutput(ReadOutputInput{Name: name, Offset: offset}, dir)
		if res.IsError {
			t.Fatalf("page %d errored: %s", pages, textContentOf(t, res))
		}
		got := textContentOf(t, res)
		pages++

		// Strip the "<name> — chars A..B of T" header line.
		nl := strings.Index(got, "\n")
		if nl < 0 {
			t.Fatalf("page %d has no header line: %q", pages, got[:80])
		}
		body := got[nl+1:]
		// Mirror the wrapper: it strips the budget-applied sentinel before the
		// agent sees the output (applyBudgetAndTook → StripBudgetMarker).
		body = mcpmeta.StripBudgetMarker(body)

		next := -1
		if i := strings.Index(body, "\n[truncated:"); i >= 0 {
			if _, err := fmt.Sscanf(body[i:], "\n[truncated: %d more chars — read_output(name=%q, offset=%d)]",
				new(int), new(string), &next); err != nil {
				t.Fatalf("page %d footer unparseable: %q", pages, body[i:i+80])
			}
			body = body[:i]
		}
		reassembled.WriteString(body)
		if next < 0 {
			break
		}
		if next <= offset {
			t.Fatalf("pagination must advance: offset %d -> %d", offset, next)
		}
		offset = next
	}

	if pages < 3 {
		t.Fatalf("expected at least 3 pages for %d chars, got %d", len(content), pages)
	}
	if reassembled.String() != content {
		t.Fatalf("reassembly mismatch: got %d chars, want %d", reassembled.Len(), len(content))
	}
}

func TestReadOutput_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "..", "outside_secret.txt")
	if err := os.WriteFile(secret, []byte("TOP-SECRET-DATA"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"../outside_secret.txt",
		"..",
		".",
		"/etc/passwd",
		"sub/../../outside_secret.txt",
		"",
	} {
		res := handleReadOutput(ReadOutputInput{Name: name}, dir)
		if !res.IsError {
			t.Fatalf("name %q must be rejected; got %q", name, truncForLog(textContentOf(t, res), 120))
		}
		if strings.Contains(textContentOf(t, res), "TOP-SECRET-DATA") {
			t.Fatalf("name %q leaked content outside the output dir", name)
		}
	}
}

func TestReadOutput_RejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("TOP-SECRET-DATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "code_graph_1.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	res := handleReadOutput(ReadOutputInput{Name: "code_graph_1.txt"}, dir)
	if !res.IsError {
		t.Fatalf("symlink escaping the output dir must be rejected; got %q", truncForLog(textContentOf(t, res), 120))
	}
	if strings.Contains(textContentOf(t, res), "TOP-SECRET-DATA") {
		t.Fatal("symlink escape leaked content outside the output dir")
	}
}

func TestReadOutput_MissingFile(t *testing.T) {
	res := handleReadOutput(ReadOutputInput{Name: "gone_1.txt"}, t.TempDir())
	if !res.IsError || !strings.Contains(textContentOf(t, res), "not found") {
		t.Fatalf("missing file must produce a clear error; got %q", textContentOf(t, res))
	}
}

func TestReadOutput_OffsetBeyondEnd(t *testing.T) {
	dir := t.TempDir()
	path, _ := saveToFile("small", "tool", dir)
	res := handleReadOutput(ReadOutputInput{Name: filepath.Base(path), Offset: 99999}, dir)
	if !res.IsError || !strings.Contains(textContentOf(t, res), "beyond end") {
		t.Fatalf("out-of-range offset must produce a clear error; got %q", textContentOf(t, res))
	}
}

// The spill notice is the contract a remote agent reads. Mutation that must
// turn this RED: drop the read_output hint from the notice (revert to the old
// bare "Use Read tool" message) — the remote path silently disappears again.
func TestSpillNotice_NamesReadOutput(t *testing.T) {
	res := largeTextResult(strings.Repeat("X", maxInlineCharsDefault*2), "code_graph", t.TempDir())
	got := textContentOf(t, res)

	if !strings.Contains(got, "saved to:") {
		t.Fatalf("fixture is inert: spill path not taken; got %q", got[:120])
	}
	if !strings.Contains(got, "read_output(") {
		t.Fatalf("spill notice must hand remote clients the read_output handle; got %q", got)
	}
	if !strings.Contains(got, "server host") {
		t.Fatalf("spill notice must state the file is server-local; got %q", got)
	}
}
