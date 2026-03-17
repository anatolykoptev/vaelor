package scip

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunIndexer executes the SCIP indexer described by cfg inside dir.
// It returns the path to the generated index.scip file.
// stdout and stderr from the subprocess are discarded.
// Returns an error if the binary is not found or the process exits non-zero.
func RunIndexer(ctx context.Context, cfg IndexerConfig, dir string) (string, error) {
	binPath, err := exec.LookPath(cfg.Name)
	if err != nil {
		return "", fmt.Errorf("scip indexer %q not found in PATH: %w", cfg.Name, err)
	}

	//nolint:gosec // cfg.Name and cfg.Args are from a controlled registry, not user input.
	cmd := exec.CommandContext(ctx, binPath, cfg.Args...)
	cmd.Dir = dir
	// Discard stdout and stderr — leave them as nil (default).

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("scip indexer %q failed: %w", cfg.Name, err)
	}

	return filepath.Join(dir, "index.scip"), nil
}

// sourceExts is the set of file extensions copied by copyForIndexing.
var sourceExts = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true, ".java": true, ".rs": true, ".rb": true, ".cs": true,
	".c": true, ".cpp": true, ".h": true, ".hpp": true,
	".json": true, ".toml": true, ".mod": true, ".sum": true,
	".lock": true, ".cfg": true, ".ini": true, ".yaml": true, ".yml": true,
}

// manifestFiles are always copied regardless of extension.
var manifestFiles = map[string]bool{
	"go.mod": true, "go.sum": true, "package.json": true,
	"tsconfig.json": true, "Cargo.toml": true, "Cargo.lock": true,
	"pyproject.toml": true, "setup.py": true, "requirements.txt": true,
	"pom.xml": true, "build.gradle": true,
}

// skipDirs are not traversed during copyForIndexing.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
}

// RunIndexerSafe runs the indexer in dir, copying to a temp dir first if dir
// is read-only. The caller must remove the temp dir when no longer needed;
// the returned path lives inside the temp dir (or directly in dir if writable).
func RunIndexerSafe(ctx context.Context, cfg IndexerConfig, dir string) (string, error) {
	workDir := dir
	if isReadOnly(dir) {
		tmp, err := os.MkdirTemp("", "go-code-scip-*")
		if err != nil {
			return "", fmt.Errorf("scip: create temp dir: %w", err)
		}
		if err := copyForIndexing(dir, tmp); err != nil {
			return "", fmt.Errorf("scip: copy sources to temp: %w", err)
		}
		workDir = tmp
	}
	return RunIndexer(ctx, cfg, workDir)
}

// isReadOnly reports whether dir is read-only by attempting to create a probe file.
func isReadOnly(dir string) bool {
	probe := filepath.Join(dir, ".scip-probe-"+randomSuffix())
	f, err := os.CreateTemp(dir, ".scip-probe-")
	if err != nil {
		return true
	}
	f.Close()
	_ = os.Remove(probe)
	_ = os.Remove(f.Name())
	return false
}

// randomSuffix returns a short pseudo-random string for probe file names.
func randomSuffix() string {
	b := make([]byte, 4)
	//nolint:gosec // non-crypto, just a probe file name
	for i := range b {
		b[i] = 'a' + byte(os.Getpid()>>uint(i*4)&0xf)
	}
	return string(b)
}

// copyForIndexing recursively copies source files from src to dst up to maxDepth=10.
// Only files with sourceExts extensions or manifest filenames are copied.
// .git, node_modules, vendor directories are skipped.
func copyForIndexing(src, dst string) error {
	return copyDir(src, dst, 0)
}

const maxCopyDepth = 10

func copyDir(src, dst string, depth int) error {
	if depth > maxCopyDepth {
		return nil
	}
	des, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", src, err)
	}
	for _, de := range des {
		name := de.Name()
		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)

		if de.IsDir() {
			if skipDirs[name] {
				continue
			}
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", dstPath, err)
			}
			if err := copyDir(srcPath, dstPath, depth+1); err != nil {
				return err
			}
			continue
		}

		ext := strings.ToLower(filepath.Ext(name))
		if !sourceExts[ext] && !manifestFiles[name] {
			continue
		}
		if err := copyFilePath(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func copyFilePath(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst) //nolint:gosec
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s → %s: %w", src, dst, err)
	}
	return out.Close()
}
