package callgraph

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser/preproc"
)

// AstroUsage represents a resolved USES relationship from an Astro file to the
// file it renders as a component.
type AstroUsage struct {
	// From is the relative path of the Astro file that contains the template ref.
	From string
	// To is the relative path of the imported component file.
	To string
	// Line is the 1-based line number of the tag usage in the Astro file.
	Line uint32
}

// ResolveTemplateRefs joins TemplateRefs against frontmatter import bindings to
// produce file-level USES relationships.
//
// It re-scans src for "import X from 'path'" statements in the frontmatter
// block, builds a binding→path map, then resolves each TemplateRef by name.
// The resolved path is made relative to root.
//
// Resolution order:
//  1. Relative imports (./…, ../…) — resolved directly against the file's directory.
//  2. Alias imports (~/…, @/…, any non-relative path) — looked up in the repo's
//     tsconfig.json compilerOptions.paths (and astro.config vite.resolve.alias as
//     fallback). The alias map is loaded once per root and cached process-wide.
//  3. Unresolved — alias was found in bindings but matched no alias prefix after
//     all attempts. The gocode_parser_unresolved_alias_total counter is incremented
//     and the ref is silently dropped (bare specifiers like 'svelte' fall here).
func ResolveTemplateRefs(src []byte, refs []preproc.TemplateRef, fileRel, root string) []AstroUsage {
	if len(refs) == 0 {
		return nil
	}
	bindings := scanFrontmatterBindings(src)
	if len(bindings) == 0 {
		return nil
	}

	// Absolute directory of the file, needed for relative-path resolution.
	fileDir := filepath.Dir(filepath.Join(root, fileRel))

	// Load alias map once per root (cached).
	aliases := loadTSConfigAliases(root)

	var out []AstroUsage
	seen := make(map[string]bool) // deduplicate (from, to) pairs per file
	for _, ref := range refs {
		importPath, ok := bindings[ref.Name]
		if !ok {
			continue
		}

		var relTarget string
		if strings.HasPrefix(importPath, ".") {
			// Relative import: resolve against the file's directory.
			absTarget := filepath.Clean(filepath.Join(fileDir, importPath))
			rel, err := filepath.Rel(root, absTarget)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			relTarget = rel
		} else {
			// Non-relative: attempt alias resolution.
			resolved, matched := resolveAlias(importPath, aliases)
			if !matched {
				// Bare specifier (e.g. 'svelte', 'astro:transitions') or unknown alias.
				// Bare specifiers are expected to be unresolvable and are not counted.
				// True aliases (contain "/") that didn't match any map entry are counted.
				if strings.Contains(importPath, "/") {
					parserUnresolvedAliasTotal.Inc()
				}
				continue
			}
			relTarget = resolved
		}

		key := fileRel + "|" + relTarget
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, AstroUsage{From: fileRel, To: relTarget, Line: ref.Line})
	}
	return out
}

// scanFrontmatterBindings parses the Astro frontmatter block (between leading
// --- fences) and returns a map of binding name → import specifier.
//
// Handles only the common ES module import forms:
//   - import Foo from './Foo.astro'       → {"Foo": "./Foo.astro"}
//   - import { A, B } from './lib'        → named exports; each gets its own entry
//   - import * as Ns from './ns'          → {"Ns": "./ns"}
//
// Lines that don't match these patterns are silently skipped.
func scanFrontmatterBindings(src []byte) map[string]string {
	// Find frontmatter region.
	trimmed := bytes.TrimLeft(src, " \t\r\n")
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		return nil
	}
	fmStart := bytes.Index(src, []byte("---")) + 3
	if fmStart < len(src) && src[fmStart] == '\r' {
		fmStart++
	}
	if fmStart < len(src) && src[fmStart] == '\n' {
		fmStart++
	}

	// Find closing ---.
	fmEnd := findFMClose(src, fmStart)
	if fmEnd <= fmStart {
		return nil
	}

	bindings := make(map[string]string)
	fm := src[fmStart:fmEnd]

	var stmtBuf strings.Builder
	inStmt := false

	for len(fm) > 0 {
		nl := bytes.IndexByte(fm, '\n')
		var line []byte
		if nl < 0 {
			line = fm
			fm = nil
		} else {
			line = fm[:nl]
			fm = fm[nl+1:]
		}
		// Strip Windows \r if present.
		line = bytes.TrimRight(line, "\r")
		trimmed := bytes.TrimSpace(line)

		if inStmt {
			// Continuation of a multi-line import statement.
			stmtBuf.WriteByte(' ')
			stmtBuf.Write(trimmed)
			// Statement is complete once the continuation line delivers " from ".
			if bytes.Contains(trimmed, []byte(" from ")) {
				parseImportLine(stmtBuf.String(), bindings)
				stmtBuf.Reset()
				inStmt = false
			}
			continue
		}

		if !bytes.HasPrefix(trimmed, []byte("import ")) {
			continue
		}
		// Start of an import statement.
		if bytes.Contains(trimmed, []byte(" from ")) {
			// Single-line — handle directly.
			parseImportLine(string(trimmed), bindings)
		} else {
			// Multi-line: accumulate until " from " appears.
			stmtBuf.Reset()
			stmtBuf.Write(trimmed)
			inStmt = true
		}
	}
	// Flush any incomplete statement (shouldn't happen in valid Astro, but be safe).
	if inStmt && stmtBuf.Len() > 0 {
		parseImportLine(stmtBuf.String(), bindings)
	}
	return bindings
}

// findFMClose finds the byte offset of the closing --- line, starting at
// fmStart. Returns fmStart if the closing fence is not found.
func findFMClose(src []byte, fmStart int) int {
	orig := fmStart
	search := src[fmStart:]
	for {
		nl := bytes.IndexByte(search, '\n')
		if nl < 0 {
			break
		}
		lineStart := nl + 1
		if bytes.HasPrefix(search[lineStart:], []byte("---")) {
			return orig + lineStart
		}
		search = search[lineStart:]
		orig += lineStart
	}
	return fmStart
}
