package upstream

import (
	"testing"
)

func TestResolve_ExactMatch(t *testing.T) {
	s, ok := Resolve("minio/minio")
	if !ok || s != "minio/minio" {
		t.Errorf("got (%q,%v), want (minio/minio,true)", s, ok)
	}
}

func TestResolve_ExactMatch_Redis(t *testing.T) {
	s, ok := Resolve("redis")
	if !ok || s != "redis/redis" {
		t.Errorf("got (%q,%v), want (redis/redis,true)", s, ok)
	}
}

func TestResolve_ExactMatch_Prometheus(t *testing.T) {
	s, ok := Resolve("prom/prometheus")
	if !ok || s != "prometheus/prometheus" {
		t.Errorf("got (%q,%v), want (prometheus/prometheus,true)", s, ok)
	}
}

func TestResolve_ExactMatch_NodeExporter(t *testing.T) {
	s, ok := Resolve("quay.io/prometheus/node-exporter")
	if !ok || s != "prometheus/node_exporter" {
		t.Errorf("got (%q,%v), want (prometheus/node_exporter,true)", s, ok)
	}
}

func TestResolve_GhcrHeuristic(t *testing.T) {
	s, ok := Resolve("ghcr.io/anatolykoptev/go-code")
	if !ok || s != "anatolykoptev/go-code" {
		t.Errorf("got (%q,%v), want (anatolykoptev/go-code,true)", s, ok)
	}
}

// ghcr.io with 3-segment path: take first 2 segments as owner/repo.
func TestResolve_GhcrHeuristic_ThreeSegments(t *testing.T) {
	s, ok := Resolve("ghcr.io/owner/repo/subpath")
	if !ok || s != "owner/repo" {
		t.Errorf("got (%q,%v), want (owner/repo,true)", s, ok)
	}
}

// ghcr.io with only 1 segment (bare owner): no repo — ok=false.
func TestResolve_GhcrHeuristic_OneSegment(t *testing.T) {
	_, ok := Resolve("ghcr.io/owner")
	if ok {
		t.Error("expected ok=false for ghcr.io/owner (no repo segment)")
	}
}

func TestResolve_Unmapped(t *testing.T) {
	_, ok := Resolve("some-random/image")
	if ok {
		t.Error("expected ok=false")
	}
}

func TestResolve_DockerHubUnmapped(t *testing.T) {
	_, ok := Resolve("ubuntu")
	if ok {
		t.Error("expected ok=false for unmapped docker hub image")
	}
}
