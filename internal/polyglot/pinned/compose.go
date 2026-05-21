package pinned

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseCompose reads a docker-compose YAML file at path and returns one
// PinnedImage per service that declares an "image" field.
//
// Variable interpolation is strict: only ${VAR:-default} forms are honoured
// (the default literal is used). ${VAR} without a default is never resolved
// against the process environment — an explicit design choice to avoid silent
// cross-environment leaks. Such variables emit a non-empty Unresolved field.
//
// Compose-spec include: directives (string form) are followed recursively.
// Map-form items ({path: foo, env_file: bar}) are skipped silently per spec.
// Cycle detection is based on absolute paths; each file is parsed at most once.
// Recursion is capped at 10 levels.
func ParseCompose(path string) ([]PinnedImage, error) {
	return parseComposeImpl(path, map[string]bool{}, 0)
}

// parseComposeImpl is the recursive inner implementation of ParseCompose.
// seen maps absolute paths to true for cycle detection.
// depth tracks recursion depth; returns nil beyond maxIncludeDepth.
//
// Source on returned PinnedImages carries the same path form as the caller
// passed: relative paths stay relative, absolute stay absolute.
// This preserves backward compatibility with Collect's filepath.Rel rewriting.
const maxIncludeDepth = 10

func parseComposeImpl(path string, seen map[string]bool, depth int) ([]PinnedImage, error) {
	if depth > maxIncludeDepth {
		// Silent skip — too deeply nested.
		return nil, nil
	}

	// Canonicalise for cycle detection only; Source uses the original path.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if seen[absPath] {
		// Silent skip — cycle.
		return nil, nil
	}
	seen[absPath] = true

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Decode into yaml.Node to access line numbers.
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	if root.Kind == 0 || len(root.Content) == 0 {
		return nil, nil
	}

	// Root is a document node; the actual mapping is root.Content[0].
	docNode := root.Content[0]
	if docNode.Kind != yaml.MappingNode {
		return nil, nil
	}

	var result []PinnedImage

	// --- 1. Walk services: block for this file ---
	servicesNode := mappingLookup(docNode, "services")
	if servicesNode != nil && servicesNode.Kind == yaml.MappingNode {
		// servicesNode is a mapping: key1, val1, key2, val2, ...
		for i := 0; i+1 < len(servicesNode.Content); i += 2 {
			svcNameNode := servicesNode.Content[i]
			svcValNode := servicesNode.Content[i+1]

			svcName := svcNameNode.Value

			// Decode the service node into a struct to handle yaml anchors/merges.
			var svc struct {
				Image string      `yaml:"image"`
				Build interface{} `yaml:"build"`
			}
			if err := svcValNode.Decode(&svc); err != nil {
				// Malformed service — skip this service but continue.
				continue
			}

			if svc.Image == "" {
				continue
			}

			// Find the image node line number by walking the service mapping.
			imageLine := imageLineFromNode(svcValNode)

			// Perform strict interpolation (no process env).
			interpolated, unresolved := interpolateCompose(svc.Image)

			pi := parseImageRef(interpolated)
			if unresolved != "" {
				// Preserve any Unresolved from interpolation; parseImageRef may
				// have its own (e.g. invalid digest) — chain them if both exist.
				if pi.Unresolved != "" {
					pi.Unresolved = unresolved + "; " + pi.Unresolved
				} else {
					pi.Unresolved = unresolved
				}
			}
			pi.Service = svcName
			pi.Line = imageLine
			pi.Source = path

			result = append(result, pi)
		}
	}

	// --- 2. Follow include: directives ---
	// Compose spec allows top-level include: to reference other compose files.
	// String items are followed recursively; map items (with path:/env_file:)
	// are skipped silently (MVP: string form only).
	//
	// Include paths are resolved relative to the directory of the *original*
	// path parameter (not its absolute form) so that relative paths stay
	// relative in the returned PinnedImages' Source field. This keeps backward
	// compatibility with Collect's filepath.Rel rewriting.
	//
	// Known limitation: if a sub-file's basename also matches the walker glob
	// AND it is referenced via include:, it may be parsed twice. Dedup at the
	// Collect level is deferred.
	includeNode := mappingLookup(docNode, "include")
	if includeNode != nil && includeNode.Kind == yaml.SequenceNode {
		// Use the original path's directory (may be relative) for constructing
		// include paths so Source preserves the caller's path form.
		baseDir := filepath.Dir(path)
		for _, item := range includeNode.Content {
			if item.Kind != yaml.ScalarNode {
				// Map-form or other non-string: skip silently per spec.
				continue
			}
			includePath := item.Value
			if !filepath.IsAbs(includePath) {
				includePath = filepath.Join(baseDir, includePath)
			}
			sub, err := parseComposeImpl(includePath, seen, depth+1)
			if err != nil {
				// Non-fatal: include file unreadable/malformed — skip.
				continue
			}
			result = append(result, sub...)
		}
	}

	return result, nil
}

// mappingLookup finds the value node for a given key in a yaml MappingNode.
func mappingLookup(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// imageLineFromNode searches for the "image" key in a service mapping node
// and returns the line number of the value node. Falls back to node.Line.
func imageLineFromNode(serviceNode *yaml.Node) int {
	if serviceNode.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(serviceNode.Content); i += 2 {
			if serviceNode.Content[i].Value == "image" {
				return serviceNode.Content[i+1].Line
			}
		}
	}
	if serviceNode.Line > 0 {
		return serviceNode.Line
	}
	return 1
}

// interpolateCompose performs strict variable interpolation on a compose image
// value. Rules:
//   - ${VAR:-default} → replaced with "default" literal
//   - ${VAR} (no default) → replaced with "" and adds an Unresolved message
//
// Process environment is NEVER consulted.
func interpolateCompose(s string) (result, unresolved string) {
	var sb strings.Builder
	var unresolvedParts []string

	for {
		start := strings.Index(s, "${")
		if start < 0 {
			sb.WriteString(s)
			break
		}
		sb.WriteString(s[:start])
		s = s[start+2:]

		end := strings.Index(s, "}")
		if end < 0 {
			// No closing brace — write literally and stop.
			sb.WriteString("${")
			sb.WriteString(s)
			break
		}

		expr := s[:end]
		s = s[end+1:]

		// Check for :- default.
		sepIdx := strings.Index(expr, ":-")
		if sepIdx >= 0 {
			// Use default value.
			sb.WriteString(expr[sepIdx+2:])
		} else {
			// No default — emit Unresolved and replace with empty string.
			unresolvedParts = append(unresolvedParts, fmt.Sprintf("${%s} has no default in compose, not honouring process env", expr))
			// sb writes nothing (empty replacement)
		}
	}

	if len(unresolvedParts) > 0 {
		unresolved = strings.Join(unresolvedParts, "; ")
	}
	result = sb.String()
	return
}
