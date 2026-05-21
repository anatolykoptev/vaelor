package fleet_test

import (
	"reflect"
	"testing"

	"github.com/anatolykoptev/go-code/internal/fleet"
	"github.com/anatolykoptev/go-code/internal/polyglot/pinned"
)

// helper: build a PinnedImage with minimal fields.
func pinnedImg(image, tag, digest, service, unresolved string) pinned.PinnedImage {
	return pinned.PinnedImage{
		Image:      image,
		Tag:        tag,
		Digest:     digest,
		Service:    service,
		Unresolved: unresolved,
	}
}

// helper: build a RuntimeImage with minimal fields.
func runtimeImg(image, tag, digest, container, service string) fleet.RuntimeImage {
	return fleet.RuntimeImage{
		Image:     image,
		Tag:       tag,
		Digest:    digest,
		Container: container,
		Service:   service,
	}
}

func TestDiff_Empty(t *testing.T) {
	t.Parallel()
	result := fleet.Diff(nil, nil)
	if len(result) != 0 {
		t.Errorf("Diff(nil,nil) = %d rows; want 0", len(result))
	}
}

func TestDiff_OnlySource(t *testing.T) {
	t.Parallel()
	result := fleet.Diff(
		[]pinned.PinnedImage{pinnedImg("docker.io/library/nginx", "1.27", "", "", "")},
		nil,
	)
	if len(result) != 1 {
		t.Fatalf("len = %d; want 1", len(result))
	}
	r := result[0]
	if r.Status != fleet.DiffOnlySource {
		t.Errorf("Status = %q; want OnlySource", r.Status)
	}
	if r.Pinned == nil {
		t.Error("Pinned is nil; want set")
	}
	if r.Runtime != nil {
		t.Errorf("Runtime is non-nil; want nil")
	}
	if r.Explanation == "" {
		t.Error("Explanation is empty; want non-empty for non-Match status")
	}
}

func TestDiff_OnlyRuntime(t *testing.T) {
	t.Parallel()
	result := fleet.Diff(
		nil,
		[]fleet.RuntimeImage{runtimeImg("docker.io/library/nginx", "1.27", "", "nginx-1", "")},
	)
	if len(result) != 1 {
		t.Fatalf("len = %d; want 1", len(result))
	}
	r := result[0]
	if r.Status != fleet.DiffOnlyRuntime {
		t.Errorf("Status = %q; want OnlyRuntime", r.Status)
	}
	if r.Runtime == nil {
		t.Error("Runtime is nil; want set")
	}
	if r.Pinned != nil {
		t.Errorf("Pinned is non-nil; want nil")
	}
	if r.Explanation == "" {
		t.Error("Explanation is empty; want non-empty for non-Match status")
	}
}

func TestDiff_Match(t *testing.T) {
	t.Parallel()
	result := fleet.Diff(
		[]pinned.PinnedImage{pinnedImg("docker.io/library/nginx", "1.27", "", "", "")},
		[]fleet.RuntimeImage{runtimeImg("docker.io/library/nginx", "1.27", "", "nginx-1", "")},
	)
	if len(result) != 1 {
		t.Fatalf("len = %d; want 1", len(result))
	}
	r := result[0]
	if r.Status != fleet.DiffMatch {
		t.Errorf("Status = %q; want Match", r.Status)
	}
	if r.Pinned == nil || r.Runtime == nil {
		t.Error("both Pinned and Runtime must be non-nil for Match")
	}
}

func TestDiff_TagDrift(t *testing.T) {
	t.Parallel()
	result := fleet.Diff(
		[]pinned.PinnedImage{pinnedImg("docker.io/library/nginx", "1.27", "", "", "")},
		[]fleet.RuntimeImage{runtimeImg("docker.io/library/nginx", "1.26", "", "nginx-1", "")},
	)
	if len(result) != 1 {
		t.Fatalf("len = %d; want 1", len(result))
	}
	if result[0].Status != fleet.DiffTagDrift {
		t.Errorf("Status = %q; want TagDrift", result[0].Status)
	}
	if result[0].Explanation == "" {
		t.Error("Explanation is empty; want non-empty for TagDrift")
	}
}

func TestDiff_DigestDrift(t *testing.T) {
	t.Parallel()
	const (
		digestA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		digestB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	result := fleet.Diff(
		[]pinned.PinnedImage{pinnedImg("docker.io/library/nginx", "1.27", digestA, "", "")},
		[]fleet.RuntimeImage{runtimeImg("docker.io/library/nginx", "1.27", digestB, "nginx-1", "")},
	)
	if len(result) != 1 {
		t.Fatalf("len = %d; want 1", len(result))
	}
	if result[0].Status != fleet.DiffDigestDrift {
		t.Errorf("Status = %q; want DigestDrift", result[0].Status)
	}
	if result[0].Explanation == "" {
		t.Error("Explanation is empty; want non-empty for DigestDrift")
	}
}

func TestDiff_Unresolved(t *testing.T) {
	t.Parallel()
	const reason = "ARG BASE_TAG is not statically resolvable"
	result := fleet.Diff(
		[]pinned.PinnedImage{pinnedImg("docker.io/library/nginx", "", "", "", reason)},
		nil,
	)
	if len(result) != 1 {
		t.Fatalf("len = %d; want 1", len(result))
	}
	r := result[0]
	if r.Status != fleet.DiffUnresolved {
		t.Errorf("Status = %q; want Unresolved", r.Status)
	}
	if r.Pinned == nil {
		t.Error("Pinned is nil; want set")
	}
	// Explanation should include the unresolved reason.
	if r.Explanation == "" {
		t.Error("Explanation is empty; want unresolved reason")
	}
}

func TestDiff_Unresolved_WithRuntime(t *testing.T) {
	t.Parallel()
	const reason = "ARG BASE_TAG is not statically resolvable"
	result := fleet.Diff(
		[]pinned.PinnedImage{pinnedImg("docker.io/library/nginx", "", "", "", reason)},
		[]fleet.RuntimeImage{runtimeImg("docker.io/library/nginx", "1.27", "", "nginx-1", "")},
	)
	if len(result) != 1 {
		t.Fatalf("len = %d; want 1 (Unresolved consumes its Runtime pair)", len(result))
	}
	r := result[0]
	if r.Status != fleet.DiffUnresolved {
		t.Errorf("Status = %q; want Unresolved", r.Status)
	}
	if r.Pinned == nil {
		t.Error("Pinned is nil; want set")
	}
	if r.Runtime == nil {
		t.Error("Runtime is nil; want set (runtime was available)")
	}
	if r.Explanation == "" {
		t.Error("Explanation empty; want unresolved reason")
	}
}

func TestDiff_MultiImage(t *testing.T) {
	t.Parallel()
	// pinned: nginx + redis; runtime: nginx + postgres
	// → nginx Match, redis OnlySource, postgres OnlyRuntime
	ps := []pinned.PinnedImage{
		pinnedImg("docker.io/library/nginx", "1.27", "", "", ""),
		pinnedImg("docker.io/library/redis", "7.2", "", "", ""),
	}
	rs := []fleet.RuntimeImage{
		runtimeImg("docker.io/library/nginx", "1.27", "", "nginx-1", ""),
		runtimeImg("docker.io/library/postgres", "16", "", "postgres-1", ""),
	}
	result := fleet.Diff(ps, rs)
	if len(result) != 3 {
		t.Fatalf("len = %d; want 3", len(result))
	}

	// Build a map for assertions (Status priority sort means TagDrift first, Match last).
	byImage := make(map[string]fleet.DiffStatus)
	for _, d := range result {
		byImage[d.Image] = d.Status
	}

	if byImage["docker.io/library/nginx"] != fleet.DiffMatch {
		t.Errorf("nginx status = %q; want Match", byImage["docker.io/library/nginx"])
	}
	if byImage["docker.io/library/redis"] != fleet.DiffOnlySource {
		t.Errorf("redis status = %q; want OnlySource", byImage["docker.io/library/redis"])
	}
	if byImage["docker.io/library/postgres"] != fleet.DiffOnlyRuntime {
		t.Errorf("postgres status = %q; want OnlyRuntime", byImage["docker.io/library/postgres"])
	}
}

func TestDiff_DuplicatePinned(t *testing.T) {
	t.Parallel()
	// 2× pinned nginx (from Dockerfile + compose), 1× runtime nginx
	// → 1 Match + 1 OnlySource
	ps := []pinned.PinnedImage{
		pinnedImg("docker.io/library/nginx", "1.27", "", "web", ""),
		pinnedImg("docker.io/library/nginx", "1.27", "", "sidecar", ""),
	}
	rs := []fleet.RuntimeImage{
		runtimeImg("docker.io/library/nginx", "1.27", "", "nginx-1", ""),
	}
	result := fleet.Diff(ps, rs)
	if len(result) != 2 {
		t.Fatalf("len = %d; want 2", len(result))
	}

	var matchCount, onlySourceCount int
	for _, d := range result {
		switch d.Status {
		case fleet.DiffMatch:
			matchCount++
		case fleet.DiffOnlySource:
			onlySourceCount++
		default:
			t.Errorf("unexpected status %q for image %q", d.Status, d.Image)
		}
	}
	if matchCount != 1 {
		t.Errorf("Match count = %d; want 1", matchCount)
	}
	if onlySourceCount != 1 {
		t.Errorf("OnlySource count = %d; want 1", onlySourceCount)
	}
}

func TestDiff_Determinism(t *testing.T) {
	t.Parallel()
	ps := []pinned.PinnedImage{
		pinnedImg("docker.io/library/nginx", "1.27", "", "web", ""),
		pinnedImg("docker.io/library/redis", "7.2", "", "", ""),
		pinnedImg("docker.io/library/postgres", "16", "", "", "not-resolvable"),
	}
	rs := []fleet.RuntimeImage{
		runtimeImg("docker.io/library/nginx", "1.26", "", "nginx-1", "web"),
		runtimeImg("docker.io/library/redis", "7.2", "", "redis-1", ""),
	}
	first := fleet.Diff(ps, rs)
	second := fleet.Diff(ps, rs)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("Diff is not deterministic:\nfirst  = %+v\nsecond = %+v", first, second)
	}
}
