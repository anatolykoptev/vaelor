package main

import "os"

// getenvRebrand reads an env var with dual-read rebrand fallback.
// VAELOR_<suffix> wins when set-and-nonempty; otherwise GO_CODE_<suffix> is
// read. Returns "" when neither is set (or both empty).
//
// This lets the deployment migrate env-var names from the GO_CODE_ prefix to
// the VAELOR_ prefix at its own pace without breaking either. GO_CODE_ support
// is NOT removed — when only GO_CODE_<suffix> is set, behavior is byte-identical
// to the pre-rebrand read.
func getenvRebrand(suffix string) string {
	if v := os.Getenv("VAELOR_" + suffix); v != "" {
		return v
	}
	return os.Getenv("GO_CODE_" + suffix)
}
