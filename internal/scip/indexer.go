package scip

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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
	return runIndexer(ctx, cfg, dir, "")
}

// outputFlagIndexers accept `--output <path>`, verified for scip-typescript
// (CommandLineOptions.js). Others write <cwd>/index.scip.
var outputFlagIndexers = map[string]bool{indexerScipTypescript: true}

// runIndexer is RunIndexer with an optional explicit output path. A non-empty
// outPath is passed via --output to indexers that support it and returned as
// the index location; otherwise the index is <dir>/index.scip.
func runIndexer(ctx context.Context, cfg IndexerConfig, dir, outPath string) (string, error) {
	binPath, err := exec.LookPath(cfg.Name)
	if err != nil {
		return "", fmt.Errorf("scip indexer %q not found in PATH: %w", cfg.Name, err)
	}

	// The indexer reads (and for rust-analyzer/scip-java may execute) content of
	// a repo we do not control, so it gets a minimal allowlisted environment and
	// a throwaway HOME — never the server's own environment (tokens, DSNs,
	// service secrets). See indexerEnv.
	home, err := os.MkdirTemp("", "go-code-scip-home-*")
	if err != nil {
		return "", fmt.Errorf("scip: create indexer home: %w", err)
	}
	defer func() {
		if rerr := os.RemoveAll(home); rerr != nil {
			slog.Warn("scip: remove indexer home failed", "path", home, "err", rerr)
		}
	}()
	realHome, _ := os.UserHomeDir()

	args := cfg.Args
	indexPath := filepath.Join(dir, "index.scip")
	if outPath != "" && outputFlagIndexers[cfg.Name] {
		args = append(append([]string{}, cfg.Args...), "--output", outPath)
		indexPath = outPath
	}
	//nolint:gosec // cfg.Name and cfg.Args are from a controlled registry, not user input.
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Dir = dir
	cmd.Env = indexerEnv(cfg.Name, home, realHome, os.Getenv)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("scip indexer %q failed: %w (output: %s)", cfg.Name, err, string(output))
	}

	return indexPath, nil
}

// sourceExts is the set of file extensions copied by copyForIndexing.
var sourceExts = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true, ".java": true, ".rs": true, ".rb": true, ".cs": true,
	".c": true, ".cpp": true, ".h": true, ".hpp": true,
	".json": true, ".toml": true, ".mod": true, ".sum": true,
	".cfg": true, ".ini": true, ".yaml": true, ".yml": true,
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
	"target": true, "__pycache__": true, ".cargo": true, ".rustup": true, ".svelte-kit": true,
	"dist": true, "build": true, ".next": true, ".nuxt": true, "out": true,
}

// IndexResult holds the path to the generated index and an optional cleanup function.
type IndexResult struct {
	IndexPath string // path to the index.scip file
	Cleanup   func() // removes temp dir if one was created; nil if dir was writable
}

// RunIndexerSafe runs the indexer against dir. Trusted roots that are writable
// are indexed in place. Everything else — read-only roots AND every untrusted
// root, writable or not — is indexed in a fresh symlink-safe copy: indexers
// write <cwd>/index.scip (and scip-typescript --infer-tsconfig writes
// tsconfig.json), and those writes follow repo symlinks, so running in place
// on attacker content is an arbitrary file write. For untrusted roots the
// index is also directed to a separate temp dir via --output where supported.
// Caller MUST call result.Cleanup() when done with the index.
func RunIndexerSafe(ctx context.Context, cfg IndexerConfig, dir string) (*IndexResult, error) {
	if IsTrustedRoot(dir) && !isReadOnly(dir) {
		indexPath, err := RunIndexer(ctx, cfg, dir)
		if err != nil {
			return nil, err
		}
		return &IndexResult{IndexPath: indexPath}, nil
	}

	var tmps []string
	cleanup := func() {
		for _, t := range tmps {
			if err := os.RemoveAll(t); err != nil {
				slog.Warn("scip: remove temp dir failed", "path", t, "err", err)
			}
		}
	}
	mk := func(pattern string) (string, error) {
		t, err := os.MkdirTemp("", pattern)
		if err == nil {
			tmps = append(tmps, t)
		}
		return t, err
	}

	workDir, err := mk("go-code-scip-*")
	if err != nil {
		return nil, fmt.Errorf("scip: create temp dir: %w", err)
	}
	if err := copyForIndexing(dir, workDir); err != nil {
		cleanup()
		return nil, fmt.Errorf("scip: copy sources to temp: %w", err)
	}
	outDir, err := mk("go-code-scip-out-*")
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("scip: create output dir: %w", err)
	}

	indexPath, err := runIndexer(ctx, cfg, workDir, filepath.Join(outDir, "index.scip"))
	if err != nil {
		cleanup()
		return nil, err
	}
	return &IndexResult{IndexPath: indexPath, Cleanup: cleanup}, nil
}

// isReadOnly reports whether dir is read-only by attempting to create a temp file.
func isReadOnly(dir string) bool {
	f, err := os.CreateTemp(dir, ".scip-probe-")
	if err != nil {
		return true
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return false
}

// copyForIndexing recursively copies source files from src to dst up to maxDepth=10.
// Only files with sourceExts extensions or manifest filenames are copied.
// .git, node_modules, vendor directories are skipped.
//
// Symlinks are never followed out of src: a repo link to /etc or a credentials
// dir would otherwise be copied into the index dir and read by the indexer.
func copyForIndexing(src, dst string) error {
	realRoot, err := filepath.EvalSymlinks(src)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", src, err)
	}
	return copyDir(realRoot, src, dst, 0)
}

const (
	maxCopyDepth = 10
	// copyDirPerm is the mode of directories created in the indexing copy.
	copyDirPerm = 0o750
)

func copyDir(realRoot, src, dst string, depth int) error {
	if depth > maxCopyDepth {
		return nil
	}
	des, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", src, err)
	}
	for _, de := range des {
		if err := copyEntry(realRoot, src, dst, de, depth); err != nil {
			return err
		}
	}
	return nil
}

// copyEntry copies one directory entry: recursing into directories, resolving
// symlinks safely, and copying wanted source files.
func copyEntry(realRoot, src, dst string, de os.DirEntry, depth int) error {
	name := de.Name()
	srcPath := filepath.Join(src, name)
	dstPath := filepath.Join(dst, name)

	switch {
	case de.Type()&os.ModeSymlink != 0:
		return copySymlink(realRoot, srcPath, dstPath, name)
	case de.IsDir():
		if skipDirs[name] {
			return nil
		}
		if err := os.MkdirAll(dstPath, copyDirPerm); err != nil {
			return fmt.Errorf("mkdir %s: %w", dstPath, err)
		}
		return copyDir(realRoot, srcPath, dstPath, depth+1)
	case wantedFile(name):
		return copyFilePath(srcPath, dstPath)
	}
	return nil
}

// wantedFile reports whether a file name is copied for indexing.
func wantedFile(name string) bool {
	return sourceExts[strings.ToLower(filepath.Ext(name))] || manifestFiles[name]
}

// copySymlink copies the file behind the symlink at srcPath when it is a
// copyable source file whose resolved target stays inside realRoot.
func copySymlink(realRoot, srcPath, dstPath, name string) error {
	if !wantedFile(name) {
		return nil // would not be copied anyway
	}
	target, ok := safeSymlinkTarget(realRoot, srcPath)
	if !ok {
		return nil
	}
	// Open the resolved target, not the link, so a swap between the check and
	// the open cannot redirect the read.
	return copyFilePath(target, dstPath)
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

// safeSymlinkTarget resolves the symlink at path and reports whether it is a
// regular file inside realRoot. Links that leave the root, dangle, loop or
// point at directories are skipped; escapes are logged and counted.
func safeSymlinkTarget(realRoot, path string) (string, bool) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		slog.Debug("scip: skipping unresolvable symlink", "path", path, "err", err)
		return "", false
	}
	if !within(realRoot, target) {
		RecordSkipped("-", SkipReasonSymlinkEscape)
		slog.Warn("scip: skipping symlink that leaves the repo root",
			"path", path, "reason", SkipReasonSymlinkEscape)
		return "", false
	}
	fi, err := os.Stat(target)
	if err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	return target, true
}
