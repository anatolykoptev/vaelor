package codegraph

import (
	"os"
	"strings"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/polyglot"
	"github.com/anatolykoptev/go-code/internal/routes"
)

// maxRoleSampleBytes limits how much source code is read per file for role
// classification.
const maxRoleSampleBytes = 500

// buildCrossLanguageData performs polyglot detection, route extraction, role
// classification, and returns cross-language vertices and edges ready for
// insertion. It also produces BELONGS_TO edges (File -> Layer).
func buildCrossLanguageData(root string, allFiles []*ingest.File) ([]vertexData, []edgeData) {
	structure := polyglot.DetectStructure(allFiles)

	routeList := extractRoutes(root, allFiles)
	classifyLayerRoles(structure.Layers, allFiles, routeList)
	fileToLayer := buildFileToLayerMap(root, allFiles, structure.Layers)

	// Build Layer/Route vertices and HANDLES/FETCHES edges.
	crossVertices, crossEdges := buildCrossLanguageGraph(structure.Layers, routeList, fileToLayer)

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
func extractRoutes(root string, allFiles []*ingest.File) []routes.Route {
	var routeList []routes.Route
	for _, f := range allFiles {
		if f.Language == "" || f.Language == "c" || f.Language == "cpp" {
			continue
		}
		src, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		fileRoutes := routes.ExtractAll(f.Language, src)
		rel := relPath(f.Path, root)
		for i := range fileRoutes {
			fileRoutes[i].File = rel
		}
		routeList = append(routeList, fileRoutes...)
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
