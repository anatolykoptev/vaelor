package fleet_test

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/fleet"
)

func TestParseImageRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		ref               string
		wantImage         string
		wantTag           string
		wantDigest        string
		wantInvalidDigest string // non-empty means we expect invalidDigestReason != ""
	}{
		{
			name:      "image only, no tag, no digest → latest",
			ref:       "alpine",
			wantImage: "alpine",
			wantTag:   "latest",
		},
		{
			name:      "image:tag",
			ref:       "nginx:1.27-alpine",
			wantImage: "nginx",
			wantTag:   "1.27-alpine",
		},
		{
			name:       "image@sha256:digest",
			ref:        "redis@sha256:abc123",
			wantImage:  "redis",
			wantTag:    "",
			wantDigest: "sha256:abc123",
		},
		{
			name:       "image:tag@sha256:digest",
			ref:        "postgres:16@sha256:abc",
			wantImage:  "postgres",
			wantTag:    "16",
			wantDigest: "sha256:abc",
		},
		{
			name:      "registry with port:tag",
			ref:       "localhost:5000/foo:1.0",
			wantImage: "localhost:5000/foo",
			wantTag:   "1.0",
		},
		{
			name:      "registry with port, no tag → latest",
			ref:       "localhost:5000/foo",
			wantImage: "localhost:5000/foo",
			wantTag:   "latest",
		},
		{
			name:      "registry with port and subpath:tag",
			ref:       "localhost:5000/foo/bar:1.0",
			wantImage: "localhost:5000/foo/bar",
			wantTag:   "1.0",
		},
		{
			name:              "invalid digest prefix silently noted",
			ref:               "registry.io/img:1.0@md5:notvalid",
			wantImage:         "registry.io/img",
			wantTag:           "1.0",
			wantDigest:        "",
			wantInvalidDigest: "md5",
		},
		{
			name:       "image with no tag but has digest — no latest applied",
			ref:        "ubuntu@sha256:deadbeef",
			wantImage:  "ubuntu",
			wantTag:    "",
			wantDigest: "sha256:deadbeef",
		},
		{
			name:      "bare image no tag",
			ref:       "busybox",
			wantImage: "busybox",
			wantTag:   "latest",
		},
		{
			name:      "multisegment image path no tag",
			ref:       "registry.example.com/org/repo",
			wantImage: "registry.example.com/org/repo",
			wantTag:   "latest",
		},
		{
			name:      "multisegment image path with tag",
			ref:       "registry.example.com/org/repo:v2.3",
			wantImage: "registry.example.com/org/repo",
			wantTag:   "v2.3",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			image, tag, digest, invalidDigestReason := fleet.ParseImageRef(tc.ref)
			if image != tc.wantImage {
				t.Errorf("image: want %q, got %q", tc.wantImage, image)
			}
			if tag != tc.wantTag {
				t.Errorf("tag: want %q, got %q", tc.wantTag, tag)
			}
			if digest != tc.wantDigest {
				t.Errorf("digest: want %q, got %q", tc.wantDigest, digest)
			}
			if tc.wantInvalidDigest != "" && invalidDigestReason == "" {
				t.Errorf("invalidDigestReason: want non-empty (contains %q), got empty", tc.wantInvalidDigest)
			}
			if tc.wantInvalidDigest == "" && invalidDigestReason != "" {
				t.Errorf("invalidDigestReason: want empty, got %q", invalidDigestReason)
			}
		})
	}
}
