package ingest

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// treeMaxLines is the maximum number of output lines before truncation.
const treeMaxLines = 100

// treeNode represents a node in the virtual directory tree.
type treeNode struct {
	name     string
	children map[string]*treeNode
	isFile   bool
}

func newTreeNode(name string) *treeNode {
	return &treeNode{name: name, children: make(map[string]*treeNode)}
}

// RenderTree builds a visual directory tree from a list of files and returns
// it as a string using box-drawing characters. Output is capped at treeMaxLines
// lines; a trailing summary line is added when truncated.
//
// Example output:
//
//	├── cmd/
//	│   └── main.go
//	├── internal/
//	│   ├── handler.go
//	│   └── config.go
//	└── go.mod
func RenderTree(files []*File) string {
	root := newTreeNode("")

	for _, f := range files {
		insertPath(root, f.RelPath)
	}

	var lines []string
	renderNode(root, "", &lines)

	if len(lines) <= treeMaxLines {
		return strings.Join(lines, "\n")
	}

	remaining := len(lines) - treeMaxLines
	truncated := lines[:treeMaxLines]
	truncated = append(truncated, fmt.Sprintf("... (%d more files)", remaining))
	return strings.Join(truncated, "\n")
}

// insertPath creates treeNode entries for every component of relPath.
func insertPath(root *treeNode, relPath string) {
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	current := root
	for i, part := range parts {
		if part == "" {
			continue
		}
		child, ok := current.children[part]
		if !ok {
			child = newTreeNode(part)
			current.children[part] = child
		}
		if i == len(parts)-1 {
			child.isFile = true
		}
		current = child
	}
}

// renderNode writes sorted tree lines for n's children into lines.
// prefix is the indentation prefix built up by parent calls.
func renderNode(n *treeNode, prefix string, lines *[]string) {
	names := make([]string, 0, len(n.children))
	for name := range n.children {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		// Directories before files, then alphabetical.
		ci := n.children[names[i]]
		cj := n.children[names[j]]
		iIsDir := !ci.isFile || len(ci.children) > 0
		jIsDir := !cj.isFile || len(cj.children) > 0
		if iIsDir != jIsDir {
			return iIsDir
		}
		return names[i] < names[j]
	})

	for idx, name := range names {
		child := n.children[name]
		isLast := idx == len(names)-1

		connector := "├── "
		childPrefix := prefix + "│   "
		if isLast {
			connector = "└── "
			childPrefix = prefix + "    "
		}

		label := name
		if len(child.children) > 0 {
			label = name + "/"
		}
		*lines = append(*lines, prefix+connector+label)

		if len(child.children) > 0 {
			renderNode(child, childPrefix, lines)
		}
	}
}
