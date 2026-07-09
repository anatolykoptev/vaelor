package freshness

import (
	"context"
	"net/http"
	"testing"
)

func TestCheckGoRuntime_Current(t *testing.T) {
	t.Parallel()
	srv := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"version":"go1.22.5","stable":true}]`))
	})
	defer srv.Close()

	// Monkey-patch not possible, so test compareGoVersions directly.
	status := compareGoVersions("1.22", "1.22.5")
	assertEqual(t, verCurrent, status)
}

func TestCheckGoRuntime_Outdated(t *testing.T) {
	t.Parallel()
	status := compareGoVersions("1.21", "1.23.0")
	if status == verCurrent || status == "" {
		t.Errorf("expected outdated status, got %q", status)
	}
}

func TestCheckGoRuntime_Empty(t *testing.T) {
	t.Parallel()
	status := CheckGoRuntime(context.Background(), http.DefaultClient, "")
	assertEqual(t, "", status)
}

func TestCompareGoVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		have, latest string
		wantCurrent  bool
	}{
		{"1.22", "1.22.5", true},
		{"1.23", "1.23.0", true},
		{"1.23", "1.22.5", true},
		{"1.21", "1.23.0", false},
		{"1.20", "1.22.0", false},
	}

	for _, tt := range tests {
		got := compareGoVersions(tt.have, tt.latest)
		if tt.wantCurrent && got != verCurrent {
			t.Errorf("compareGoVersions(%q, %q) = %q, want %q", tt.have, tt.latest, got, verCurrent)
		}
		if !tt.wantCurrent && got == verCurrent {
			t.Errorf("compareGoVersions(%q, %q) = %q, want outdated", tt.have, tt.latest, got)
		}
	}
}

func TestFetchLatestGoVersion_Server(t *testing.T) {
	t.Parallel()
	srv := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"version":"go1.23rc1","stable":false},
			{"version":"go1.22.5","stable":true},
			{"version":"go1.21.12","stable":true}
		]`))
	})
	defer srv.Close()

	// Can't test fetchLatestGoVersion directly since it uses DefaultGoDLURL.
	// Test via the HTTP server handler to verify JSON parsing.
	var releases []goRelease
	err := registryGet(context.Background(), srv.Client(), srv.URL, &releases)
	assertNoError(t, err)

	if len(releases) != 3 {
		t.Fatalf("got %d releases, want 3", len(releases))
	}

	// Find first stable.
	var latest string
	for _, r := range releases {
		if r.Stable {
			latest = r.Version
			break
		}
	}
	assertEqual(t, "go1.22.5", latest)
}
