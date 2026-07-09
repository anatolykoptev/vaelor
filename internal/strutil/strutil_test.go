package strutil

import "testing"

func TestCommonPrefixLen(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 3},
		{"abc", "abx", 2},
		{"abc", "xyz", 0},
		{"abc", "ab", 2},
		{"ab", "abc", 2},
		{"", "abc", 0},
		{"abc", "", 0},
	}
	for _, tc := range tests {
		got := CommonPrefixLen(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("CommonPrefixLen(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestTextHash(t *testing.T) {
	t.Parallel()
	h1 := TextHash("hello")
	h2 := TextHash("hello")
	h3 := TextHash("world")
	if h1 != h2 {
		t.Error("same input must produce same hash")
	}
	if h1 == h3 {
		t.Error("different inputs must produce different hashes (collision unlikely for these strings)")
	}
	if h1 == 0 {
		t.Error("hash of non-empty string must be non-zero")
	}
	if TextHash("") == 0 {
		// FNV-64a of empty string is the offset basis (non-zero)
		t.Error("hash of empty string should be non-zero (FNV offset basis)")
	}
}
