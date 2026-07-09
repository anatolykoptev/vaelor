package ssh_test

import (
	"testing"
	"time"

	flssh "github.com/anatolykoptev/go-code/internal/fleet/ssh"
)

func TestParseDockerPSLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		line            string
		wantContainer   string
		wantImage       string
		wantTag         string
		wantDigest      string
		wantService     string
		wantStartedAtZ  bool // true = expect zero time
		wantStartedNonZ bool // true = expect non-zero time
	}{
		{
			name:          "Names populated",
			line:          `{"ID":"abc123","Names":"web","Image":"nginx:1.27-alpine","State":"running","Labels":"","CreatedAt":"2024-08-12 14:00:00 +0000 UTC"}`,
			wantContainer: "web",
			wantImage:     "nginx",
			wantTag:       "1.27-alpine",
		},
		{
			name:           "Names empty fallback to short ID",
			line:           `{"ID":"0123456789abcdefghij","Names":"","Image":"alpine","State":"running","Labels":"","CreatedAt":""}`,
			wantContainer:  "0123456789ab",
			wantImage:      "alpine",
			wantTag:        "latest",
			wantStartedAtZ: true,
		},
		{
			name:          "Image with registry port",
			line:          `{"ID":"abc","Names":"custom","Image":"localhost:5000/foo/bar:1.0","State":"running","Labels":"","CreatedAt":""}`,
			wantContainer: "custom",
			wantImage:     "localhost:5000/foo/bar",
			wantTag:       "1.0",
		},
		{
			name:          "Image with registry port no tag",
			line:          `{"ID":"abc","Names":"custom","Image":"localhost:5000/foo/bar","State":"running","Labels":"","CreatedAt":""}`,
			wantContainer: "custom",
			wantImage:     "localhost:5000/foo/bar",
			wantTag:       "latest",
		},
		{
			name:          "Digest only",
			line:          `{"ID":"abc","Names":"db","Image":"redis@sha256:abc123","State":"running","Labels":"","CreatedAt":""}`,
			wantContainer: "db",
			wantImage:     "redis",
			wantTag:       "",
			wantDigest:    "sha256:abc123",
		},
		{
			name:          "Tag and digest",
			line:          `{"ID":"abc","Names":"db","Image":"postgres:16@sha256:abc","State":"running","Labels":"","CreatedAt":""}`,
			wantContainer: "db",
			wantImage:     "postgres",
			wantTag:       "16",
			wantDigest:    "sha256:abc",
		},
		{
			name:          "Compose service label",
			line:          `{"ID":"abc","Names":"web","Image":"nginx","State":"running","Labels":"com.docker.compose.service=web,com.docker.compose.project=myapp","CreatedAt":""}`,
			wantContainer: "web",
			wantImage:     "nginx",
			wantTag:       "latest",
			wantService:   "web",
		},
		{
			name:          "Empty labels",
			line:          `{"ID":"abc","Names":"web","Image":"nginx","State":"running","Labels":"","CreatedAt":""}`,
			wantContainer: "web",
			wantService:   "",
		},
		{
			name:          "Labels missing compose service key",
			line:          `{"ID":"abc","Names":"web","Image":"nginx","State":"running","Labels":"com.docker.compose.project=myapp","CreatedAt":""}`,
			wantContainer: "web",
			wantService:   "",
		},
		{
			name:          "Compose service not first label",
			line:          `{"ID":"abc","Names":"svc","Image":"nginx","State":"running","Labels":"k1=v1,com.docker.compose.service=api","CreatedAt":""}`,
			wantContainer: "svc",
			wantService:   "api",
		},
		{
			name:            "CreatedAt parses to non-zero",
			line:            `{"ID":"abc","Names":"web","Image":"nginx","State":"running","Labels":"","CreatedAt":"2024-08-12 14:00:00 +0000 UTC"}`,
			wantContainer:   "web",
			wantStartedNonZ: true,
		},
		{
			name:           "CreatedAt empty is zero time",
			line:           `{"ID":"abc","Names":"web","Image":"nginx","State":"running","Labels":"","CreatedAt":""}`,
			wantContainer:  "web",
			wantStartedAtZ: true,
		},
		{
			name:           "CreatedAt garbage is zero time",
			line:           `{"ID":"abc","Names":"web","Image":"nginx","State":"running","Labels":"","CreatedAt":"garbage"}`,
			wantContainer:  "web",
			wantStartedAtZ: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			img, err := flssh.ParseDockerPSLine([]byte(tc.line))
			if err != nil {
				t.Fatalf("ParseDockerPSLine(%q): unexpected error: %v", tc.line, err)
			}
			if tc.wantContainer != "" && img.Container != tc.wantContainer {
				t.Errorf("Container: want %q, got %q", tc.wantContainer, img.Container)
			}
			if tc.wantImage != "" && img.Image != tc.wantImage {
				t.Errorf("Image: want %q, got %q", tc.wantImage, img.Image)
			}
			if tc.wantTag != "" && img.Tag != tc.wantTag {
				t.Errorf("Tag: want %q, got %q", tc.wantTag, img.Tag)
			}
			if tc.wantDigest != "" && img.Digest != tc.wantDigest {
				t.Errorf("Digest: want %q, got %q", tc.wantDigest, img.Digest)
			}
			if tc.wantService != "" && img.Service != tc.wantService {
				t.Errorf("Service: want %q, got %q", tc.wantService, img.Service)
			}
			if tc.wantStartedAtZ && !img.StartedAt.Equal(time.Time{}) {
				t.Errorf("StartedAt: want zero, got %v", img.StartedAt)
			}
			if tc.wantStartedNonZ && img.StartedAt.IsZero() {
				t.Errorf("StartedAt: want non-zero, got zero")
			}
		})
	}
}

func TestParseLabels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		labels  string
		wantSvc string
	}{
		{"empty", "", ""},
		{"project only", "com.docker.compose.project=myapp", ""},
		{"service present", "com.docker.compose.service=web,com.docker.compose.project=myapp", "web"},
		{"service second", "k1=v1,com.docker.compose.service=api", "api"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := flssh.ParseComposeService(tc.labels)
			if got != tc.wantSvc {
				t.Errorf("ParseComposeService(%q): want %q, got %q", tc.labels, tc.wantSvc, got)
			}
		})
	}
}
