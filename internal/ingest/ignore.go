package ingest

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// defaultIgnoreDirs lists directories that should never be descended into.
var defaultIgnoreDirs = map[string]bool{
	".git":           true,
	".claude":        true,
	"node_modules":   true,
	"vendor":         true,
	"__pycache__":    true,
	".venv":          true,
	"venv":           true,
	".env":           true,
	"dist":           true,
	"build":          true,
	".next":          true,
	".nuxt":          true,
	".svelte-kit":    true,
	".output":        true,
	".cache":         true,
	".idea":          true,
	".vscode":        true,
	".vs":            true,
	".terraform":     true,
	"Pods":           true,
	".gradle":        true,
	"target":         true,
	".tox":           true,
	".mypy_cache":    true,
	".pytest_cache":  true,
	".cargo":         true,
	"zig-cache":      true,
	"zig-out":        true,
	"testdata":       true,
}

// binaryExtensions lists file extensions that are always skipped.
var binaryExtensions = map[string]bool{
	".exe":  true,
	".dll":  true,
	".so":   true,
	".dylib": true,
	".a":    true,
	".o":    true,
	// compiled bytecode
	".pyc":  true,
	".pyo":  true,
	".class": true,
	".jar":  true,
	".war":  true,
	// images
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".bmp":  true,
	".ico":  true,
	".svg":  true,
	".webp": true,
	".tiff": true,
	// audio/video
	".mp3":  true,
	".mp4":  true,
	".avi":  true,
	".mov":  true,
	".wav":  true,
	// archives
	".zip":  true,
	".tar":  true,
	".gz":   true,
	".bz2":  true,
	".xz":   true,
	".rar":  true,
	".7z":   true,
	// fonts
	".woff":  true,
	".woff2": true,
	".ttf":   true,
	".eot":   true,
	// documents
	".pdf":  true,
	".doc":  true,
	".docx": true,
	".xls":  true,
	".xlsx": true,
	// databases
	".sqlite": true,
	".db":     true,
	// wasm and minified assets
	".wasm":    true,
	".min.js":  true,
	".min.css": true,
}

// generatedSuffixes lists file suffixes that indicate auto-generated code.
// These files are skipped because they add noise without useful signal.
var generatedSuffixes = []string{
	".pb.go",
	".pb.gw.go",
	"_generated.go",
	"_gen.go",
	".gen.go",
	".generated.go",
}

// generatedDirs lists directory names that contain generated/migration files
// which are typically not useful for code analysis.
var generatedDirs = map[string]bool{
	"migrations": true,
	"migration":  true,
}

// ignoreFiles lists filenames that are always skipped regardless of extension.
var ignoreFiles = map[string]bool{
	// lock files
	"package-lock.json": true,
	"yarn.lock":         true,
	"pnpm-lock.yaml":    true,
	"go.sum":            true,
	"Cargo.lock":        true,
	"Gemfile.lock":      true,
	"poetry.lock":       true,
	"composer.lock":     true,
	// dot files
	".gitignore":     true,
	".gitattributes": true,
	".editorconfig":  true,
	".DS_Store":      true,
	"Thumbs.db":      true,
}

// binarySniffLen is how many bytes we read to detect binary content.
const binarySniffLen = 512

func shouldIgnoreDir(name string) bool {
	return defaultIgnoreDirs[name] || generatedDirs[name]
}

// shouldIgnoreFile returns true when the file should be skipped based on its
// name (exact match) or extension (binary extension list).
// It also handles compound extensions like ".min.js".
func shouldIgnoreFile(name string) bool {
	if ignoreFiles[name] {
		return true
	}
	// Check compound extensions (.min.js, .min.css) first.
	for ext := range binaryExtensions {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	// Skip generated code files (protobuf, codegen, etc.).
	for _, suffix := range generatedSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// isBinaryData returns true when the byte slice looks like binary content.
// It inspects only the first binarySniffLen bytes for null bytes.
func isBinaryData(data []byte) bool {
	limit := len(data)
	if limit > binarySniffLen {
		limit = binarySniffLen
	}
	for _, b := range data[:limit] {
		if b == 0 {
			return true
		}
	}
	return false
}

// parseGitignore reads the .gitignore in root and returns its non-empty,
// non-comment lines as patterns.
func parseGitignore(root string) []string {
	path := filepath.Join(root, ".gitignore")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

// matchGitignore reports whether relPath (relative to repo root) matches any
// of the given gitignore patterns.
// It uses filepath.Match for glob patterns and also tries a prefix match for
// directory-style patterns ending in "/".
func matchGitignore(relPath string, isDir bool, patterns []string) bool {
	name := filepath.Base(relPath)
	for _, pattern := range patterns {
		// Negation patterns are not supported — we only ignore.
		if strings.HasPrefix(pattern, "!") {
			continue
		}

		dirOnly := strings.HasSuffix(pattern, "/")
		if dirOnly && !isDir {
			continue
		}
		p := strings.TrimSuffix(pattern, "/")

		// Pattern with slash inside → match against full relative path.
		if strings.ContainsRune(p, '/') {
			if ok, _ := filepath.Match(p, relPath); ok {
				return true
			}
			// Also check as prefix (directory match).
			if isDir && strings.HasPrefix(relPath+"/", p+"/") {
				return true
			}
			continue
		}

		// No slash → match against the base name only.
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}
