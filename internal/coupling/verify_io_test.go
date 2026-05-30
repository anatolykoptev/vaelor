package coupling

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadVerifyFile(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("handler.rs", `fn main() {}`)
	write("README.md", `# docs`)
	write("VERSION", `1.2.3`)

	t.Run("source file returns bytes and language", func(t *testing.T) {
		src, lang := readVerifyFile(dir, "handler.rs")
		if lang != "rust" {
			t.Errorf("lang = %q, want rust", lang)
		}
		if len(src) == 0 {
			t.Error("expected bytes, got empty")
		}
	})
	t.Run("markdown gives empty lang (skip signal)", func(t *testing.T) {
		src, lang := readVerifyFile(dir, "README.md")
		if lang != "" {
			t.Errorf("lang = %q, want empty", lang)
		}
		if src != nil {
			t.Errorf("src = %v, want nil on skip", src) // pins (nil,"") contract; kills dropped-guard mutant
		}
	})
	t.Run("extensionless VERSION gives empty lang", func(t *testing.T) {
		src, lang := readVerifyFile(dir, "VERSION")
		if lang != "" {
			t.Errorf("lang = %q, want empty", lang)
		}
		if src != nil {
			t.Errorf("src = %v, want nil on skip", src) // pins (nil,"") contract; kills dropped-guard mutant
		}
	})
	t.Run("missing file returns nil/empty", func(t *testing.T) {
		src, lang := readVerifyFile(dir, "nope.rs")
		if src != nil || lang != "" {
			t.Errorf("missing file: got src=%v lang=%q, want nil/empty", src, lang)
		}
	})
	t.Run("oversized file skipped", func(t *testing.T) {
		big := make([]byte, maxVerifyFileBytes+1)
		write("big.go", string(big))
		src, lang := readVerifyFile(dir, "big.go")
		if src != nil || lang != "" {
			t.Errorf("oversized: got src len=%d lang=%q, want nil/empty", len(src), lang)
		}
	})
}
