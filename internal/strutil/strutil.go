// Package strutil provides small string utilities shared across packages.
package strutil

import "hash/fnv"

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
