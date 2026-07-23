package main

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// mainGaugeValue reads the value of the named unlabeled gauge from the default
// Prometheus registry. Returns 0 when no sample exists.
func mainGaugeValue(t *testing.T, metricName string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			_ = dto.Metric{}
			return m.GetGauge().GetValue()
		}
	}
	return 0
}

// --- #3: learnings DB fallback gauge (#594, #610 gap 3) ---------------------
//
// LEARNINGS_DATABASE_URL silently falls back to DATABASE_URL when unset
// (config.go), co-locating review learnings with the main DB without an operator
// signal. gocode_learnings_db_fallback must publish: 0=dedicated, 1=fallback,
// 2=disabled. Falsification: break learningsFallbackState or remove the publish
// and the gauge value goes wrong (RED).

func TestLearningsFallbackState(t *testing.T) {
	cases := []struct {
		name         string
		learningsURL string
		databaseURL  string
		want         float64
	}{
		{"dedicated: LEARNINGS_DATABASE_URL set", "postgres://learn", "postgres://main", 0},
		{"dedicated: LEARNINGS set, DATABASE unset", "postgres://learn", "", 0},
		{"fallback: LEARNINGS unset, DATABASE set", "", "postgres://main", 1},
		{"disabled: both unset", "", "", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := learningsFallbackState(tc.learningsURL, tc.databaseURL); got != tc.want {
				t.Errorf("learningsFallbackState(%q, %q) = %v, want %v", tc.learningsURL, tc.databaseURL, got, tc.want)
			}
		})
	}
}

func TestPublishLearningsDBFallback_SetsGauge(t *testing.T) {
	// Not parallel: process-global gauge, last-writer-wins must be sequential.
	const metric = "gocode_learnings_db_fallback"
	cases := []struct {
		name         string
		learningsURL string
		databaseURL  string
		want         float64
	}{
		{"dedicated", "postgres://learn", "postgres://main", 0},
		{"fallback", "", "postgres://main", 1},
		{"disabled", "", "", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			publishLearningsDBFallback(tc.learningsURL, tc.databaseURL)
			if got := mainGaugeValue(t, metric); got != tc.want {
				t.Errorf("gauge = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestLoadConfig_PublishesLearningsFallback drives the REAL production config
// path (loadConfig) and asserts the gauge reflects the env. Falsification: remove
// the publishLearningsDBFallback call in loadConfig and the gauge stays stale.
func TestLoadConfig_PublishesLearningsFallback(t *testing.T) {
	const metric = "gocode_learnings_db_fallback"

	t.Run("fallback when only DATABASE_URL set", func(t *testing.T) {
		t.Setenv("LEARNINGS_DATABASE_URL", "")
		t.Setenv("DATABASE_URL", "postgres://test_main_db")
		if _, err := loadConfig(); err != nil {
			t.Fatalf("loadConfig: %v", err)
		}
		if got := mainGaugeValue(t, metric); got != 1 {
			t.Errorf("gocode_learnings_db_fallback = %v, want 1 (fallback)", got)
		}
	})

	t.Run("dedicated when LEARNINGS_DATABASE_URL set", func(t *testing.T) {
		t.Setenv("LEARNINGS_DATABASE_URL", "postgres://test_learnings_db")
		t.Setenv("DATABASE_URL", "postgres://test_main_db")
		if _, err := loadConfig(); err != nil {
			t.Fatalf("loadConfig: %v", err)
		}
		if got := mainGaugeValue(t, metric); got != 0 {
			t.Errorf("gocode_learnings_db_fallback = %v, want 0 (dedicated)", got)
		}
	})

	t.Run("disabled when both unset", func(t *testing.T) {
		t.Setenv("LEARNINGS_DATABASE_URL", "")
		t.Setenv("DATABASE_URL", "")
		if _, err := loadConfig(); err != nil {
			t.Fatalf("loadConfig: %v", err)
		}
		if got := mainGaugeValue(t, metric); got != 2 {
			t.Errorf("gocode_learnings_db_fallback = %v, want 2 (disabled)", got)
		}
	})
}

// --- #6: sparse embedder active gauge (#602, #610 gap 6) --------------------
//
// SPARSE_EMBED_URL misconfiguration (or vocab-size mismatch) silently disables
// the sparse retrieval arm — the pipeline stays dense-only with no signal.
// gocode_sparse_embedder_active must publish 1 when the sparse embedder is
// wired into the pipeline, 0 when nil. Drives the REAL production wiring path
// (wireSparse, called by newSemanticDeps). Falsification: remove the publish in
// wireSparse and the gauge stays 0 (RED for the enabled case).
func TestWireSparse_PublishesActiveGauge(t *testing.T) {
	// Not parallel: process-global gauge, last-writer-wins must be sequential.
	const metric = "gocode_sparse_embedder_active"

	t.Run("disabled when SPARSE_EMBED_URL empty", func(t *testing.T) {
		cfg := Config{SparseEmbedURL: "", SparseEmbedModel: "splade-v3-distilbert", SparseEmbedMaxArray: 32}
		sc, _ := wireSparse(cfg, zeroRRFWeights())
		if sc != nil {
			t.Fatal("wireSparse with empty URL should return nil embedder")
		}
		if got := mainGaugeValue(t, metric); got != 0 {
			t.Errorf("gocode_sparse_embedder_active = %v, want 0 (disabled)", got)
		}
	})

	t.Run("enabled when SPARSE_EMBED_URL set", func(t *testing.T) {
		cfg := Config{SparseEmbedURL: "http://127.0.0.1:9999/embed_sparse", SparseEmbedModel: "splade-v3-distilbert", SparseEmbedMaxArray: 32}
		sc, _ := wireSparse(cfg, zeroRRFWeights())
		if sc == nil {
			t.Fatal("wireSparse with set URL should return non-nil embedder")
		}
		if got := mainGaugeValue(t, metric); got != 1 {
			t.Errorf("gocode_sparse_embedder_active = %v, want 1 (enabled)", got)
		}
	})
}

// zeroRRFWeights returns a zero-value RRFWeights for the wireSparse log path.
func zeroRRFWeights() embeddings.RRFWeights {
	return embeddings.RRFWeights{}
}
