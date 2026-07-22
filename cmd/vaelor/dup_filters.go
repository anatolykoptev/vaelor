package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/anatolykoptev/vaelor/internal/semhealth"
)

// dupFilterOpts controls post-triage group filtering for find_duplicates
// (issue #568). All fields optional; a zero value means the filter is inactive.
type dupFilterOpts struct {
	Language  string  // e.g. "go", "python" — inferred from file extension
	Path      string  // repo-relative dir prefix; symbols outside it are dropped
	Threshold float64 // minimum AvgSimilarity (exact tier sim=1.0 always passes)
	MinLines  int     // drop groups where any symbol body is shorter than this
	Root      string  // on-disk repo root, for MinLines file reads
}

// filterDupGroups applies the optional language / path / threshold / min_lines
// filters to a group slice and returns the narrowed slice. A group is kept only
// when it passes EVERY active filter. Filters with zero values are inactive.
func filterDupGroups(groups []semhealth.DupGroup, opts dupFilterOpts) []semhealth.DupGroup {
	if opts.Language == "" && opts.Path == "" && opts.Threshold <= 0 && opts.MinLines <= 0 {
		return groups
	}
	pathPrefix := normalizePathPrefix(opts.Path)
	// Cache symbol line counts by (file, startLine) so a symbol shared across
	// groups is read once. Bounded by the number of unique symbols in the result
	// set (already capped by defaultDupLimit / the triage cap).
	lineCache := make(map[string]int)

	out := groups[:0:0]
	for _, g := range groups {
		if !groupPasses(g, opts, pathPrefix, lineCache) {
			continue
		}
		out = append(out, g)
	}
	return out
}

func groupPasses(g semhealth.DupGroup, opts dupFilterOpts, pathPrefix string, lineCache map[string]int) bool {
	// Threshold: exact tier (sim=1.0) always passes; otherwise require >= threshold.
	if opts.Threshold > 0 && g.Tier != dupTierExact && float64(g.AvgSimilarity) < opts.Threshold {
		return false
	}
	for _, s := range g.Symbols {
		if opts.Language != "" && languageOfFile(s.File) != strings.ToLower(opts.Language) {
			return false
		}
		if pathPrefix != "" && !pathHasPrefix(s.File, pathPrefix) {
			return false
		}
		if opts.MinLines > 0 {
			key := s.File + ":" + itoa(s.Line)
			n, ok := lineCache[key]
			if !ok {
				n = symbolLineCount(opts.Root, s.File, s.Line)
				lineCache[key] = n
			}
			if n < opts.MinLines {
				return false
			}
		}
	}
	return true
}

// normalizePathPrefix cleans a repo-relative dir prefix and ensures it does not
// start with a slash (DupSymbol.File is repo-relative, no leading slash).
func normalizePathPrefix(p string) string {
	p = filepath.ToSlash(filepath.Clean(p))
	if p == "" || p == "." || p == "/" {
		return ""
	}
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return ""
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

// pathHasPrefix reports whether a repo-relative file path is under prefix.
// Both sides are cleaned to slash form to tolerate trailing-slash / "./" noise.
func pathHasPrefix(file, prefix string) bool {
	file = filepath.ToSlash(filepath.Clean(file))
	return strings.HasPrefix(file, prefix) || file == strings.TrimSuffix(prefix, "/")
}

// languageOfFile infers a language id from a file extension. Returns "" for
// unknown extensions so the Language filter drops them (conservative: a file
// we can't classify does not match any requested language).
func languageOfFile(file string) string {
	ext := strings.ToLower(filepath.Ext(file))
	if ext == "" {
		return ""
	}
	// Same extension→language mapping the parser uses (single source of truth
	// is the tree-sitter handler set); mirrored here as a leaf lookup so the
	// find_duplicates filter does not import the parser package.
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".pyi":
		return "python"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescript"
	case ".js":
		return "javascript"
	case ".jsx":
		return "javascript"
	case ".mjs", ".cjs":
		return "javascript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".kt", ".kts":
		return "kotlin"
	case ".swift":
		return "swift"
	case ".c":
		return "c"
	case ".h":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hpp", ".hh":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".cs":
		return "csharp"
	case ".php":
		return "php"
	case ".vue":
		return "vue"
	case ".svelte":
		return "svelte"
	case ".astro":
		return "astro"
	case ".html", ".htm":
		return "html"
	default:
		return ""
	}
}

// symbolLineCount is a best-effort estimate of a symbol's body line count,
// read from the on-disk file at root/file starting at startLine. It scans
// forward from startLine for the first opening brace and counts to its
// matching close (braced languages), or to the next column-0 definition line
// for indentation-scoped languages (python/ruby). Returns 0 on any read error
// or when no body boundary is found within the scan cap.
//
// The scan is capped at dupMinLinesScanCap lines to bound cost on huge files;
// a body longer than the cap is reported as the cap (so min_lines filters
// against large bodies still pass).
const dupMinLinesScanCap = 5000

func symbolLineCount(root, file string, startLine int) int {
	if root == "" || file == "" || startLine <= 0 {
		return 0
	}
	path := filepath.Join(root, filepath.FromSlash(file))
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// Position the scanner at startLine (1-indexed).
	lineNo := 0
	for lineNo < startLine-1 {
		if !scanner.Scan() {
			return 0 // EOF before startLine
		}
		lineNo++
	}
	// Advance to startLine itself.
	if !scanner.Scan() {
		return 0
	}

	lang := languageOfFile(file)
	braced := isBracedLanguage(lang)

	depth := 0
	opened := false
	count := 0

	for {
		text := scanner.Text()
		count++
		if count > dupMinLinesScanCap {
			return count
		}
		if braced {
			for _, r := range text {
				if r == '{' {
					depth++
					opened = true
				} else if r == '}' {
					depth--
					if opened && depth <= 0 {
						return count
					}
				}
			}
		} else {
			// Indentation-scoped: the first line (count==1) is the def line
			// itself; subsequent column-0 non-blank lines end the body.
			if count > 1 && lineStartsAtCol0(text) {
				return count - 1
			}
		}
		if !scanner.Scan() {
			return count // EOF closes the body
		}
	}
}

// lineStartsAtCol0 reports whether a line is non-blank, non-comment, and begins
// at column 0 — the heuristic end-of-block signal for python/ruby.
func lineStartsAtCol0(s string) bool {
	if s == "" {
		return false
	}
	first := rune(s[0])
	if unicode.IsSpace(first) {
		return false
	}
	// Comments / decorators / line-continuation markers are not block ends.
	if first == '#' || first == '@' {
		return false
	}
	return true
}

// isBracedLanguage reports whether a language uses brace-delimited blocks.
func isBracedLanguage(lang string) bool {
	switch lang {
	case "python", "ruby":
		return false
	default:
		return lang != ""
	}
}

func itoa(n int) string {
	// Tiny helper to avoid importing strconv just for a cache key.
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
