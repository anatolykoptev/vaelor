package main

import "testing"

// TestGetenvRebrand_VaelorWinsWhenBothSet proves precedence: when both
// VAELOR_<X> and GO_CODE_<X> are set-and-nonempty, the VAELOR_ value wins.
func TestGetenvRebrand_VaelorWinsWhenBothSet(t *testing.T) {
	t.Setenv("VAELOR_DUALREAD_PROBE", "vaelor-val")
	t.Setenv("GO_CODE_DUALREAD_PROBE", "gocode-val")
	if got := getenvRebrand("DUALREAD_PROBE"); got != "vaelor-val" {
		t.Fatalf("getenvRebrand: both set, got %q want %q", got, "vaelor-val")
	}
}

// TestGetenvRebrand_GoCodeOnly proves byte-identical legacy behavior: when
// only GO_CODE_<X> is set, it is returned unchanged.
func TestGetenvRebrand_GoCodeOnly(t *testing.T) {
	t.Setenv("VAELOR_DUALREAD_LEGACY", "")
	t.Setenv("GO_CODE_DUALREAD_LEGACY", "gocode-val")
	if got := getenvRebrand("DUALREAD_LEGACY"); got != "gocode-val" {
		t.Fatalf("getenvRebrand: GO_CODE_ only, got %q want %q", got, "gocode-val")
	}
}

// TestGetenvRebrand_NeitherSet proves the empty-default contract: when neither
// var is set (or both empty), the helper returns "".
func TestGetenvRebrand_NeitherSet(t *testing.T) {
	t.Setenv("VAELOR_DUALREAD_EMPTY", "")
	t.Setenv("GO_CODE_DUALREAD_EMPTY", "")
	if got := getenvRebrand("DUALREAD_EMPTY"); got != "" {
		t.Fatalf("getenvRebrand: neither set, got %q want empty", got)
	}
}
