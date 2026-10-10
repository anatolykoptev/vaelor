package policy

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

func TestLoad_RefusesSymlinkedPolicy(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, "severity: error\n")
	for name, target := range map[string]string{"outside file": outside, "device": fsutiltest.EndlessDevice} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			fsutiltest.Symlink(t, root, ".go-code.yaml", target)
			var p *Policy
			var err error
			fsutiltest.Within(t, func() { p, err = Load(root) })
			if err != nil || p != nil {
				t.Fatalf("symlinked policy must read as absent, got %+v, %v", p, err)
			}
		})
	}
}

func TestLoad_RegularPolicyStillLoads(t *testing.T) {
	root := t.TempDir()
	fsutiltest.WriteFile(t, root, ".go-code.yaml", "severity: error\n")
	p, err := Load(root)
	if err != nil || p == nil || p.Severity != "error" {
		t.Fatalf("got %+v, %v", p, err)
	}
}
