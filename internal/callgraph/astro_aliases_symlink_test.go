package callgraph

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

func TestBuildAliasMap_RefusesSymlinkedConfigs(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, `{"compilerOptions":{"paths":{"@canary/*":["canary/*"]}}}`)
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "tsconfig.json", outside)
	fsutiltest.Symlink(t, root, "tsconfig.base.json", fsutiltest.EndlessDevice)
	fsutiltest.Symlink(t, root, "astro.config.mjs", fsutiltest.EndlessDevice)

	var m aliasMap
	var files []string
	fsutiltest.Within(t, func() { m, files, _ = buildAliasMap(root) })
	if len(m) != 0 || len(files) != 0 {
		t.Fatalf("symlinked configs must contribute nothing: %v %v", m, files)
	}
}
