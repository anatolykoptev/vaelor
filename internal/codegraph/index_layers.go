package codegraph

import (
	"os"
	"strconv"
	"strings"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/langutil"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/polyglot"
	"github.com/anatolykoptev/go-code/internal/routes"
)

// symbolSpan captures the line-span of a function or method symbol, used by
// resolveEnclosingSymbol to find the innermost function containing a route
// registration line.
type symbolSpan struct {
	name      string
	file      string
	startLine uint32
	endLine   uint32
}

// buildFileSymbols groups function and method symbols from the parsed symbol
// slice into a per-file map of symbolSpans. Only function/method kinds are
// included because the enclosing-fn resolver needs callable symbols whose span
// can contain a route registration line.
func buildFileSymbols(allSymbols []*parser.Symbol) map[string][]symbolSpan {
	out := make(map[string][]symbolSpan)
	for _, s := range allSymbols {
		if s.Kind != parser.KindFunction && s.Kind != parser.KindMethod {
			continue
		}
		if s.StartLine == 0 || s.EndLine == 0 {
			continue
		}
		rel := s.File // Symbol.File is absolute; callers that use relative paths
		// must normalise before calling resolveEnclosingSymbol.
		out[rel] = append(out[rel], symbolSpan{
			name:      s.Name,
			file:      rel,
			startLine: s.StartLine,
			endLine:   s.EndLine,
		})
	}
	return out
}

// resolveEnclosingSymbol returns the name of the innermost function/method
// symbol in fileSymbols[file] whose [startLine, endLine] span contains line.
// "Innermost" means the symbol with the smallest span (endLine - startLine).
// If multiple symbols have the same span size (unlikely but possible), the
// last one wins (stable under append order, adequate for the resolver's purpose).
// Returns ("", false) when no symbol spans the given line.
func resolveEnclosingSymbol(fileSymbols map[string][]symbolSpan, file string, line uint32) (string, bool) {
	spans, ok := fileSymbols[file]
	if !ok {
		return "", false
	}
	var best *symbolSpan
	var bestSpan uint32
	for i := range spans {
		s := &spans[i]
		if line < s.startLine || line > s.endLine {
			continue
		}
		size := s.endLine - s.startLine
		if best == nil || size <= bestSpan {
			best = s
			bestSpan = size
		}
	}
	if best == nil {
		return "", false
	}
	return best.name, true
}

// maxRoleSampleBytes limits how much source code is read per file for role
// classification.
const maxRoleSampleBytes = 500

// buildCrossLanguageData performs polyglot detection, route extraction, role
// classification, and returns cross-language vertices and edges ready for
// insertion. It also produces BELONGS_TO edges (File -> Layer).
//
// allSymbols is the full parsed symbol slice from ingestAndParse; it is used
// to build the per-file function symbol index required by the enclosing-fn
// resolver in buildCrossLanguageGraph.
func buildCrossLanguageData(root string, allFiles []*ingest.File, allSymbols []*parser.Symbol) ([]vertexData, []edgeData) {
	structure := polyglot.DetectStructure(allFiles)

	repo := graphName(root)
	routeList := extractRoutes(root, allFiles, repo)
	classifyLayerRoles(structure.Layers, allFiles, routeList)
	fileToLayer := buildFileToLayerMap(root, allFiles, structure.Layers)

	// Build per-file function symbol index for the enclosing-fn resolver.
	fileSymbols := buildFileSymbols(allSymbols)
	// Routes use relative file paths (r.File is relative to root), but
	// parser.Symbol.File is absolute. Re-key fileSymbols by relative path so
	// resolveEnclosingSymbol receives the same key form as r.File.
	relFileSymbols := make(map[string][]symbolSpan, len(fileSymbols))
	for absPath, spans := range fileSymbols {
		rel := relPath(absPath, root)
		relFileSymbols[rel] = spans
	}

	// Build Layer/Route vertices and HANDLES/FETCHES edges.
	crossVertices, crossEdges := buildCrossLanguageGraph(repo, structure.Layers, routeList, fileToLayer, relFileSymbols)

	// Append BELONGS_TO edges (File -> Layer).
	for file, layerName := range fileToLayer {
		crossEdges = append(crossEdges, edgeData{
			FromLabel: "File",
			FromKey:   file,
			ToLabel:   "Layer",
			ToKey:     layerName,
			EdgeLabel: "BELONGS_TO",
			Props:     map[string]string{},
		})
	}

	return crossVertices, crossEdges
}

// extractRoutes reads source files and extracts HTTP routes across all
// supported languages (excluding C/C++ which have no route matchers).
//
// Guards applied (both bump counters via repo label):
//  1. Test files (langutil.IsTestFile) — skipped entirely; reason "test_file".
//  2. Junk paths (routes.IsJunkPath) — routes dropped post-extraction; reason "junk".
//
// Kept routes bump routesExtractedTotal{repo, framework, side}.
func extractRoutes(root string, allFiles []*ingest.File, repo string) []routes.Route {
	var routeList []routes.Route
	for _, f := range allFiles {
		if f.Language == "" || f.Language == "c" || f.Language == "cpp" {
			continue
		}
		// Guard 1: skip test files entirely.
		if langutil.IsTestFile(f.Path) {
			recordRouteRejected(repo, "test_file")
			continue
		}
		src, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		rel := relPath(f.Path, root)
		for _, r := range routes.ExtractAll(f.Language, src) {
			// Guard 2: drop junk paths.
			if routes.IsJunkPath(r.Path) {
				recordRouteRejected(repo, "junk")
				continue
			}
			r.File = rel
			recordRoutesExtracted(repo, r.Framework, r.Side)
			routeList = append(routeList, r)
		}
	}
	return routeList
}

// classifyLayerRoles assigns roles to layers using two strategies:
// 1. Source snippet sampling → polyglot.ClassifyLayerRole
// 2. Route-based fallback → override "library" with "server"/"client" if routes found.
func classifyLayerRoles(layers []polyglot.Layer, allFiles []*ingest.File, routeList []routes.Route) {
	layerSources := sampleLayerSources(layers, allFiles)
	serverCount, clientCount := countRoutesPerLayer(layers, routeList)

	for i := range layers {
		l := &layers[i]
		if samples, ok := layerSources[l.Name]; ok {
			l.Role = polyglot.ClassifyLayerRole(samples)
		}
		// Override role based on route extraction if snippet-based detection
		// didn't find a specific role.
		if l.Role == "library" {
			if serverCount[l.Name] > 0 {
				l.Role = sideServer
			} else if clientCount[l.Name] > 0 {
				l.Role = sideClient
			}
		}
	}
}

// sampleLayerSources reads a small prefix of each source file and groups it
// by layer name for role classification.
func sampleLayerSources(layers []polyglot.Layer, allFiles []*ingest.File) map[string][]string {
	layerSources := make(map[string][]string)
	for _, f := range allFiles {
		if f.Language == "" {
			continue
		}
		for i := range layers {
			l := &layers[i]
			if matchesLayer(f.RelPath, l.RootDir) {
				src, err := os.ReadFile(f.Path)
				if err == nil {
					limit := maxRoleSampleBytes
					if len(src) < limit {
						limit = len(src)
					}
					layerSources[l.Name] = append(layerSources[l.Name], string(src[:limit]))
				}
				break
			}
		}
	}
	return layerSources
}

// countRoutesPerLayer counts server-side and client-side routes belonging to
// each layer based on file path matching.
func countRoutesPerLayer(layers []polyglot.Layer, routeList []routes.Route) (server, client map[string]int) {
	server = make(map[string]int)
	client = make(map[string]int)
	for _, r := range routeList {
		for _, l := range layers {
			if matchesLayer(r.File, l.RootDir) {
				switch r.Side {
				case sideServer:
					server[l.Name]++
				case sideClient:
					client[l.Name]++
				}
				break
			}
		}
	}
	return server, client
}

// buildFileToLayerMap maps each file's relative path to its containing layer.
func buildFileToLayerMap(root string, allFiles []*ingest.File, layers []polyglot.Layer) map[string]string {
	fileToLayer := make(map[string]string)
	for _, f := range allFiles {
		rel := relPath(f.Path, root)
		for _, l := range layers {
			if matchesLayer(f.RelPath, l.RootDir) {
				fileToLayer[rel] = l.Name
				break
			}
		}
	}
	return fileToLayer
}

// extractHookRoutes collects WordPress hook routes from PHP files and converts
// them to callgraph.HookRoute for injection into the call graph.
func extractHookRoutes(root string, allFiles []*ingest.File) []callgraph.HookRoute {
	var out []callgraph.HookRoute
	for _, f := range allFiles {
		if f.Language != "php" {
			continue
		}
		src, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		for _, r := range routes.ExtractAll("php", src) {
			if r.Framework != "wordpress" {
				continue
			}
			out = append(out, callgraph.HookRoute{
				Method:  r.Method,
				Path:    r.Path,
				Handler: r.Handler,
				Side:    r.Side,
				Line:    r.Line,
			})
		}
	}
	return out
}

// matchesLayer reports whether a relative file path belongs to the given layer
// root directory.
func matchesLayer(relPath, rootDir string) bool {
	if rootDir == "" || rootDir == "." {
		return true
	}
	return strings.HasPrefix(relPath, rootDir+"/") || relPath == rootDir
}

// htmxFetchesFromKey returns the Symbol composite key ("name:file") for a
// client-side htmx Route so that AGE's unwindEdgeMatch("Symbol", "fk") can
// split it into the name and file properties required for a MATCH.
// Returns "" when Handler is empty (callers must skip the edge in that case).
func htmxFetchesFromKey(r routes.Route) string {
	if r.Handler == "" {
		return ""
	}
	return r.Handler + ":" + r.File
}

// handlesFromKey returns the composite "Handler:File" key for the server-side
// HANDLES edge from a Symbol vertex to a Route vertex. Symbol vertices are
// keyed by "name:file"; HANDLES needs to match against that composite, same
// as FETCHES (see htmxFetchesFromKey for the parallel fix on client side).
//
// Constraint: assumes the handler symbol is defined in the same file where
// the route is registered (typical Go pattern — e.g. go-nerv internal/admin/handler.go
// registers and defines all handlers in one file). For codebases where registration
// and handler definition live in different files, Wave 7+ would need to look
// up the actual definition file via the Symbol index.
func handlesFromKey(r routes.Route) string {
	if r.Handler == "" || r.File == "" {
		return ""
	}
	return r.Handler + ":" + r.File
}

// buildCrossLanguageGraph constructs Layer and Route vertices, plus HANDLES
// and FETCHES edges connecting symbols to routes.
//
// repo is the graph name (graphName(root)) used for observability counters.
//
// fileSymbols is a per-file map of function/method symbol spans (keyed by
// relative path, same form as r.File). It is used by the enclosing-fn resolver
// as a fallback when r.Handler is empty (arrow callbacks, inline handlers, fetch
// calls that don't name a function). Named handlers (go-nerv pattern) bypass
// the resolver entirely — their path is byte-identical to the previous behaviour.
func buildCrossLanguageGraph(repo string, layers []polyglot.Layer, routeList []routes.Route, fileToLayer map[string]string, fileSymbols map[string][]symbolSpan) ([]vertexData, []edgeData) {
	var vertices []vertexData
	var edges []edgeData

	// Layer vertices.
	for _, l := range layers {
		vertices = append(vertices, vertexData{
			Label: "Layer",
			Props: map[string]string{
				"name":     l.Name,
				"role":     l.Role,
				"language": l.Language,
				"root_dir": l.RootDir,
			},
		})
	}

	// Route vertices — deduplicated by Method+":"+Path.
	routeSeen := make(map[string]bool)
	for _, r := range routeList {
		key := r.Method + ":" + r.Path
		if routeSeen[key] {
			continue
		}
		routeSeen[key] = true
		vertices = append(vertices, vertexData{
			Label: "Route",
			Props: map[string]string{
				"method":    r.Method,
				"path":      r.Path,
				"framework": r.Framework,
			},
		})
	}

	// HANDLES / FETCHES edges (Symbol → Route).
	// Both use composite "handler:file" Symbol keys so that AGE's
	// unwindEdgeMatch("Symbol", "fk") can split on ':' into name+file.
	//
	// Hybrid resolution strategy:
	//   1. Named handler present (r.Handler != "") — use it directly (go-nerv
	//      path; byte-identical to pre-CG-T3 behaviour; resolver NOT invoked).
	//   2. Handler empty + enclosing fn found — use the enclosing fn's name.
	//   3. Handler empty + no enclosing fn — drop the edge, bump unresolved counter.
	for _, r := range routeList {
		handlerName := r.Handler
		if handlerName == "" {
			// Fallback: resolve the innermost function symbol whose span
			// contains the route's source line.
			if name, ok := resolveEnclosingSymbol(fileSymbols, r.File, r.Line); ok {
				handlerName = name
			} else {
				recordRouteHandlerUnresolved(repo)
				continue
			}
		}

		// Build a synthetic Route with the resolved handler name so that the
		// existing handlesFromKey / htmxFetchesFromKey helpers produce the
		// correct "name:file" composite key without modification.
		resolved := r
		resolved.Handler = handlerName

		routeKey := r.Method + ":" + r.Path
		edgeLabel := "HANDLES"
		fromKey := handlesFromKey(resolved)
		if r.Side == sideClient {
			edgeLabel = "FETCHES"
			fromKey = htmxFetchesFromKey(resolved)
		}
		edges = append(edges, edgeData{
			FromLabel: "Symbol",
			FromKey:   fromKey,
			ToLabel:   "Route",
			ToKey:     routeKey,
			EdgeLabel: edgeLabel,
			Props: map[string]string{
				"line": strconv.Itoa(int(r.Line)),
			},
		})
		recordRouteEdgeBuilt(repo, edgeLabel)
	}

	return vertices, edges
}
