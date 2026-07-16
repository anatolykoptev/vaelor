// Package strutil provides small string utilities shared across packages.
package strutil

import (
	"encoding/binary"
	"encoding/hex"
	"hash/fnv"
)

// CommonPrefixLen returns the length of the longest common byte prefix of a and b.
func CommonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// TextHash computes an FNV-64a hash of text.
func TextHash(text string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(text))
	return h.Sum64()
}

// RepoKey produces a stable repo identifier from a repo path using FNV-32a.
// Format: code_<8-char-hex>, e.g. "code_a3f2b1c0". Used as a prometheus label
// for per-repo metrics (implementsEdgesTotal, etc.) and as the AGE graph name.
// Shared between codegraph.GraphNameFor and callgraph.extractGoImplements so
// both produce the same key for the same repo without a cross-package import.
func RepoKey(repoPath string) string {
	h := fnv.New32a()
	h.Write([]byte(repoPath))
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], h.Sum32())
	return "code_" + hex.EncodeToString(buf[:])
}
