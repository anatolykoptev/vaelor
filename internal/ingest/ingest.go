// Package ingest handles repository ingestion: cloning remote repos, walking
// the local filesystem, filtering files by language/size/gitignore rules,
// and producing a normalized file list for downstream parsing.
package ingest

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// File represents a single source file ready for parsing.
type File struct {
	// Path is the absolute path to the file on disk.
	Path string

	// RelPath is the path relative to the repository root.
	RelPath string

	// Language is the detected programming language (e.g. "go", "python").
	Language string

	// Size is the file size in bytes.
	Size int64

	// ModTime is the file modification time.
	ModTime time.Time
}

// IngestOpts controls how a repository is ingested.
type IngestOpts struct {
	// Root is the repository root directory (must already be on disk).
	Root string

	// Focus limits ingestion to files matching this glob pattern or subdirectory.
	// Empty means ingest everything.
	Focus string

	// Languages limits ingestion to files of these languages.
	// Empty means accept all supported languages.
	Languages []string

	// MaxFileBytes skips files larger than this size. 0 means no limit.
	MaxFileBytes int64

	// FollowSymlinks controls whether symlinks are followed.
	FollowSymlinks bool

	// ExcludeTests skips test files (*_test.go) when true.
	ExcludeTests bool
}

// IngestResult contains all files found after filtering.
type IngestResult struct {
	// Files is the ordered list of source files.
	Files []*File

	// Root is the repository root used for ingestion.
	Root string

	// TotalBytes is the total size of all ingested files.
	TotalBytes int64

	// SkippedCount is the number of files skipped (too large, ignored, binary).
	SkippedCount int
}

// maxWalkDepth is the maximum directory depth before descending is stopped.
const maxWalkDepth = 20

// maxWalkFiles is the maximum number of files collected in one walk.
const maxWalkFiles = 10_000

// IngestRepo walks a local repository directory and returns all source files
// that match the given options.
//
// It does NOT read file contents — content loading happens at the parse stage.
func IngestRepo(ctx context.Context, opts IngestOpts) (*IngestResult, error) {
	root := filepath.Clean(opts.Root)
	gitignorePatterns := parseGitignore(root)

	result := &IngestResult{Root: root}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible entries without aborting
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		relPath, _ := filepath.Rel(root, path)

		if d.IsDir() {
			return handleDir(relPath, d.Name(), gitignorePatterns)
		}

		// Skip symlinks unless explicitly requested.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		skipped, file := handleFile(relPath, path, d, opts, gitignorePatterns)
		if skipped {
			result.SkippedCount++
			return nil
		}
		if file == nil {
			return nil
		}

		if len(result.Files) < maxWalkFiles {
			result.Files = append(result.Files, file)
			result.TotalBytes += file.Size
		}

		return nil
	})

	return result, err
}

// handleDir decides whether a directory should be skipped entirely.
func handleDir(relPath, name string, patterns []string) error {
	if relPath == "." {
		return nil
	}
	if shouldIgnoreDir(name) || matchGitignore(relPath, true, patterns) {
		return filepath.SkipDir
	}
	depth := strings.Count(relPath, string(filepath.Separator))
	if depth >= maxWalkDepth {
		return filepath.SkipDir
	}
	return nil
}

// handleFile processes a single regular file entry.
// Returns (true, nil) when the file should be counted as skipped,
// (false, nil) when it is silently excluded (language filter, focus),
// and (false, *File) when it is accepted.
func handleFile(relPath, absPath string, d fs.DirEntry, opts IngestOpts, patterns []string) (skipped bool, f *File) {
	name := d.Name()

	if shouldIgnoreFile(name) || matchGitignore(relPath, false, patterns) {
		return true, nil
	}

	if opts.ExcludeTests && strings.HasSuffix(name, "_test.go") {
		return false, nil
	}

	lang := DetectLanguage(name)
	if len(opts.Languages) > 0 && !containsString(opts.Languages, lang) {
		return false, nil
	}

	info, _ := d.Info()
	size := int64(0)
	modTime := time.Time{}
	if info != nil {
		size = info.Size()
		modTime = info.ModTime()
	}

	if opts.MaxFileBytes > 0 && size > opts.MaxFileBytes {
		return true, nil
	}

	if opts.Focus != "" {
		matched, _ := filepath.Match(opts.Focus, relPath)
		if !matched && !strings.HasPrefix(relPath, opts.Focus) {
			return false, nil
		}
	}

	return false, &File{
		Path:     absPath,
		RelPath:  relPath,
		Language: lang,
		Size:     size,
		ModTime:  modTime,
	}
}

// containsString reports whether slice contains s (case-sensitive).
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// DetectLanguage returns the programming language for the given filename
// based on its extension.
func DetectLanguage(filename string) string {
	ext := filepath.Ext(filename)
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".cs":
		return "csharp"
	default:
		return ""
	}
}
