package callgraph

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTinyRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := `package tiny

func A() int { return B() }
func B() int { return 1 }
`
	if err := os.WriteFile(filepath.Join(dir, "tiny.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Issue #905: a cold repo must report not-ready, and Prefetch must converge
// it to ready by completing a background build — so a caller answering
// "building" today gets a warm cgCache on retry.
func TestPrefetch_ConvergesToReady(t *testing.T) {
	dir := writeTinyRepo(t)
	input := TraceRepoInput{Root: dir, Language: "go"}

	InvalidateBuildCache()
	if ReadyForBuild(input) {
		t.Fatal("fresh repo must not be ready")
	}

	Prefetch(input)
	Prefetch(input) // deduplicated: second call must not block or panic

	deadline := time.Now().Add(60 * time.Second)
	for !ReadyForBuild(input) {
		if time.Now().After(deadline) {
			t.Fatal("prefetch did not converge repo to ready within 60s")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// ReadyForBuild must stay false while a build is merely in flight —
// only a completed build may flip it, otherwise the gate would release
// callers into the same cold path it exists to avoid.
func TestReadyForBuild_ColdIsFalse(t *testing.T) {
	dir := writeTinyRepo(t)
	InvalidateBuildCache()
	if ReadyForBuild(TraceRepoInput{Root: dir}) {
		t.Error("never-built repo reported ready")
	}
}
