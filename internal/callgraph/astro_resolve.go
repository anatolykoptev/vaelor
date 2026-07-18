package callgraph

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/parser/preproc"
)

// TemplateUsage represents a resolved USES relationship from a component file
// (Astro or Svelte) to the file it renders as a child component.
type TemplateUsage struct {
	// From is the relative path of the file that contains the template ref.
	From string
	// To is the relative path of the imported component file.
	To string
	// Line is the 1-based line number of the tag usage in the source file.
	Line uint32
}

// ResolveTemplateRefs joins TemplateRefs against a file's import bindings to
// produce file-level USES relationships.
//
// The import-binding source depends on the file kind:
//   - Astro (.astro and any non-.svelte): imports declared in the leading ---
//     frontmatter block (scanFrontmatterBindings).
//   - Svelte (.svelte): imports declared in the component's <script> blocks
//     (scanSvelteScriptBindings).
//
// The binding→ref join and path resolution below are generic over both kinds.
//
// Resolution order:
//  1. Relative imports (./…, ../…) — resolved directly against the file's directory.
//  2. Alias imports (~/…, @/…, any non-relative path) — looked up in the repo's
//     tsconfig.json compilerOptions.paths (and astro.config vite.resolve.alias as
//     fallback). The alias map is loaded once per root and cached process-wide.
//  3. Unresolved — either (a) no alias key matched (bare/scoped npm package such
//     as 'svelte', '@guide/core', 'astro:transitions') — silently dropped without
//     incrementing the counter; or (b) an alias prefix matched but the resolved
//     path does not exist on disk — the gocode_parser_unresolved_alias_total
//     counter is incremented and the ref is dropped (broken declared alias).
func ResolveTemplateRefs(src []byte, refs []preproc.TemplateRef, fileRel, root string) []TemplateUsage {
	if len(refs) == 0 {
		return nil
	}

	// Import bindings live in different regions per file kind: Svelte declares them
	// in <script> blocks, Astro (and everything else) in the --- frontmatter.
	var bindings map[string]string
	if strings.HasSuffix(fileRel, ".svelte") {
		bindings = scanSvelteScriptBindings(src)
	} else {
		bindings = scanFrontmatterBindings(src)
	}
	if len(bindings) == 0 {
		return nil
	}

	// Absolute directory of the file, needed for relative-path resolution.
	fileDir := filepath.Dir(filepath.Join(root, fileRel))

	// Load alias map once per root (cached).
	aliases := loadTSConfigAliases(root)

	var out []TemplateUsage
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
				// No alias prefix matched — this is a bare or scoped npm/workspace
				// package (e.g. 'svelte', '@guide/core', 'astro:transitions').
				// These are unresolvable by design; do NOT increment the counter.
				// The counter is for declared aliases that fail, not for npm packages.
				continue
			}
			// Alias prefix matched — but does the resolved file actually exist?
			// A missing file means a declared tsconfig paths entry is broken; count it
			// so operators can discover misconfigured aliases in indexed repos.
			if _, err := os.Stat(filepath.Join(root, resolved)); err != nil {
				parserUnresolvedAliasTotal.Inc()
				continue
			}
			relTarget = resolved
		}

		key := fileRel + "|" + relTarget
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, TemplateUsage{From: fileRel, To: relTarget, Line: ref.Line})
	}
	return out
}

// scanFrontmatterBindings parses the Astro frontmatter block (between leading
// --- fences) and returns a map of binding name → import specifier.
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
	return scanImportBindings(src[fmStart:fmEnd])
}

// scanSvelteScriptBindings extracts ESM import bindings from a Svelte component's
// <script> blocks — the Svelte analogue of scanFrontmatterBindings.
//
// It reuses preproc.ExtractSvelte to obtain the concatenated <script> source (the
// same extraction the parser uses to build symbols), then parses import statements
// from it. Returns nil when the component has no <script> content.
func scanSvelteScriptBindings(src []byte) map[string]string {
	vs := preproc.ExtractSvelte(src)
	if vs == nil || len(vs.Code) == 0 {
		return nil
	}
	return scanImportBindings(vs.Code)
}

// scanImportBindings parses ES module import statements from a region of code —
// an Astro frontmatter block or a Svelte <script> block — and returns a map of
// binding name → import specifier.
//
// Handles only the common ES module import forms:
//   - import Foo from './Foo.astro'       → {"Foo": "./Foo.astro"}
//   - import { A, B } from './lib'        → named exports; each gets its own entry
//   - import * as Ns from './ns'          → {"Ns": "./ns"}
//
// Multi-line import statements are accumulated until the " from " clause appears.
// Lines that don't match these patterns are silently skipped.
func scanImportBindings(region []byte) map[string]string {
	bindings := make(map[string]string)
	fm := region

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
	// Flush any incomplete statement (shouldn't happen in valid source, but be safe).
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
