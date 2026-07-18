package fleet

import (
	"testing"
)

// fakeReport satisfies TargetReportLike for testing.
type fakeReport struct {
	target string
	diffs  []ImageDiff
}

func (f *fakeReport) TargetStr() string      { return f.target }
func (f *fakeReport) DiffsList() []ImageDiff { return f.diffs }

// mockReport is a test helper that builds a *fakeReport from a target string
// and a slice of ImageDiff.
func mockReport(target string, diffs []ImageDiff) TargetReportLike {
	return &fakeReport{target: target, diffs: diffs}
}

// TestSiblingDiff_NoDriftReturnsNil: same image+tag on both hosts → no drift.
func TestSiblingDiff_NoDriftReturnsNil(t *testing.T) {
	t.Parallel()
	r1 := mockReport("ssh://a", []ImageDiff{
		{Image: "nginx", Runtime: &RuntimeImage{Image: "nginx", Tag: "1.27"}, Status: DiffMatch},
	})
	r2 := mockReport("ssh://b", []ImageDiff{
		{Image: "nginx", Runtime: &RuntimeImage{Image: "nginx", Tag: "1.27"}, Status: DiffMatch},
	})
	got := SiblingDiff([]TargetReportLike{r1, r2})
	if len(got) != 0 {
		t.Errorf("expected empty, got %d rows", len(got))
	}
}

// TestSiblingDiff_TagDrift: same image, different tags across two hosts → one drift row.
func TestSiblingDiff_TagDrift(t *testing.T) {
	t.Parallel()
	r1 := mockReport("ssh://host-a", []ImageDiff{
		{Image: "minio/minio", Runtime: &RuntimeImage{Image: "minio/minio", Tag: "latest"}, Status: DiffMatch},
	})
	r2 := mockReport("ssh://host-b", []ImageDiff{
		{Image: "minio/minio", Runtime: &RuntimeImage{Image: "minio/minio", Tag: "26.5.3"}, Status: DiffMatch},
	})
	got := SiblingDiff([]TargetReportLike{r1, r2})
	if len(got) != 1 {
		t.Fatalf("got %d rows; want 1", len(got))
	}
	if got[0].Image != "minio/minio" || len(got[0].Variants) != 2 {
		t.Errorf("unexpected: %+v", got[0])
	}
}

// TestSiblingDiff_OnlySourceVariantsSkipped: OnlySource rows (no Runtime) must not
// contribute to sibling drift comparison — only actually-running containers count.
func TestSiblingDiff_OnlySourceVariantsSkipped(t *testing.T) {
	t.Parallel()
	r1 := mockReport("ssh://a", []ImageDiff{
		// No Runtime — this is a pinned-only row; skip.
		{Image: "redis", Status: DiffOnlySource},
	})
	r2 := mockReport("ssh://b", []ImageDiff{
		// Has Runtime but no Pinned — this is runtime-only; does have a runtime.
		{Image: "redis", Runtime: &RuntimeImage{Image: "redis", Tag: "8"}, Status: DiffOnlyRuntime},
	})
	got := SiblingDiff([]TargetReportLike{r1, r2})
	if len(got) != 0 {
		t.Errorf("OnlySource shouldn't contribute, got %d", len(got))
	}
}

// TestSiblingDiff_SingleTargetEmptyResult: one report → no cross-host comparison possible.
func TestSiblingDiff_SingleTargetEmptyResult(t *testing.T) {
	t.Parallel()
	r1 := mockReport("ssh://a", []ImageDiff{
		{Image: "x", Runtime: &RuntimeImage{Image: "x", Tag: "1"}, Status: DiffMatch},
	})
	got := SiblingDiff([]TargetReportLike{r1})
	if len(got) != 0 {
		t.Errorf("single target → no drift, got %d", len(got))
	}
}

// TestSiblingDiff_DeterministicOrder: output sorted by Image asc, then Variant.Target asc.
func TestSiblingDiff_DeterministicOrder(t *testing.T) {
	t.Parallel()
	r1 := mockReport("ssh://b", []ImageDiff{
		{Image: "redis", Runtime: &RuntimeImage{Image: "redis", Tag: "7"}, Status: DiffMatch},
		{Image: "nginx", Runtime: &RuntimeImage{Image: "nginx", Tag: "1.27"}, Status: DiffMatch},
	})
	r2 := mockReport("ssh://a", []ImageDiff{
		{Image: "redis", Runtime: &RuntimeImage{Image: "redis", Tag: "8"}, Status: DiffMatch},
		{Image: "nginx", Runtime: &RuntimeImage{Image: "nginx", Tag: "1.26"}, Status: DiffMatch},
	})
	got := SiblingDiff([]TargetReportLike{r1, r2})
	if len(got) != 2 {
		t.Fatalf("got %d; want 2", len(got))
	}
	// Sort by Image asc, then Variant.Target asc.
	if got[0].Image != "nginx" || got[1].Image != "redis" {
		t.Errorf("not sorted by Image: %v %v", got[0].Image, got[1].Image)
	}
	if got[0].Variants[0].Target != "ssh://a" {
		t.Errorf("not sorted by Target in Variants: %v", got[0].Variants)
	}
}

// TestSiblingDiff_SameTagEmptyDigestVsRealDigest: same tag but one side has empty
// digest and other has real digest → NOT drift (we don't have full info on one side).
func TestSiblingDiff_SameTagEmptyDigestVsRealDigest(t *testing.T) {
	t.Parallel()
	r1 := mockReport("ssh://a", []ImageDiff{
		{Image: "alpine", Runtime: &RuntimeImage{Image: "alpine", Tag: "3.19", Digest: "sha256:abc"}, Status: DiffMatch},
	})
	r2 := mockReport("ssh://b", []ImageDiff{
		{Image: "alpine", Runtime: &RuntimeImage{Image: "alpine", Tag: "3.19", Digest: ""}, Status: DiffMatch},
	})
	got := SiblingDiff([]TargetReportLike{r1, r2})
	if len(got) != 0 {
		t.Errorf("same-tag + one-side-no-digest should not be drift, got %d rows", len(got))
	}
}

// TestSiblingDiff_DigestDriftSameTag: same tag, both have digests but they differ → drift.
func TestSiblingDiff_DigestDriftSameTag(t *testing.T) {
	t.Parallel()
	r1 := mockReport("ssh://a", []ImageDiff{
		{Image: "postgres", Runtime: &RuntimeImage{Image: "postgres", Tag: "16", Digest: "sha256:aaa"}, Status: DiffMatch},
	})
	r2 := mockReport("ssh://b", []ImageDiff{
		{Image: "postgres", Runtime: &RuntimeImage{Image: "postgres", Tag: "16", Digest: "sha256:bbb"}, Status: DiffMatch},
	})
	got := SiblingDiff([]TargetReportLike{r1, r2})
	if len(got) != 1 {
		t.Fatalf("same-tag + different real digests → drift; got %d", len(got))
	}
}

// TestSiblingDiff_MultipleContainersSameImage: one host has two containers for the
// same image; they should be deduplicated to one variant per host in the drift row.
func TestSiblingDiff_MultipleContainersSameImage(t *testing.T) {
	t.Parallel()
	r1 := mockReport("ssh://a", []ImageDiff{
		{Image: "nginx", Runtime: &RuntimeImage{Image: "nginx", Tag: "1.27", Container: "web1"}, Status: DiffMatch},
		{Image: "nginx", Runtime: &RuntimeImage{Image: "nginx", Tag: "1.27", Container: "web2"}, Status: DiffMatch},
	})
	r2 := mockReport("ssh://b", []ImageDiff{
		{Image: "nginx", Runtime: &RuntimeImage{Image: "nginx", Tag: "1.26", Container: "web"}, Status: DiffMatch},
	})
	got := SiblingDiff([]TargetReportLike{r1, r2})
	if len(got) != 1 {
		t.Fatalf("got %d rows; want 1", len(got))
	}
	// ssh://a should appear once (we pick one representative per target per image).
	aCount := 0
	for _, v := range got[0].Variants {
		if v.Target == "ssh://a" {
			aCount++
		}
	}
	if aCount != 1 {
		t.Errorf("ssh://a appears %d times; want 1 (deduplication per target)", aCount)
	}
}
