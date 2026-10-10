package scip_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
	gocodescip "github.com/anatolykoptev/vaelor/internal/scip"
)

// The production wiring (cmd/vaelor/register.go) feeds the SCIP trust list into
// the Go typed-load env. Composed with the real predicate: an operator-managed
// checkout (a symlinked path included) keeps the inherited env — private-module
// credentials — while a clone elsewhere gets the allowlist.
//
// Mutation that must turn it RED: make GoEnv (goanalysis/loader.go) always take
// the untrusted branch, or stop installing the predicate (SetRootTrust never
// called, as when register.go loses its wiring line).
func TestGoEnvFollowsScipTrust(t *testing.T) {
	trusted := t.TempDir()
	repo := filepath.Join(trusted, "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()

	gocodescip.SetTrustedRoots([]string{trusted})
	goanalysis.SetRootTrust(gocodescip.IsTrustedRoot)
	t.Cleanup(func() {
		gocodescip.SetTrustedRoots(nil)
		goanalysis.SetRootTrust(nil)
	})
	t.Setenv("VAELOR_CANARY_SECRET", "s3cr3t-canary")

	has := func(dir string) bool {
		for _, kv := range goanalysis.GoEnv(dir) {
			if strings.HasPrefix(kv, "VAELOR_CANARY_SECRET=") {
				return true
			}
		}
		return false
	}
	if !has(repo) {
		t.Error("trusted checkout lost the inherited env (private-module credentials would not reach go)")
	}
	if has(other) {
		t.Error("untrusted root received the server env")
	}
}
