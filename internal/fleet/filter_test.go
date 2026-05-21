package fleet_test

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/fleet"
)

func TestIsValidFilter(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty string", "", true},
		{"letters only", "nginx", true},
		{"with tag separator", "web-api", true},
		{"dot allowed", "my.service", true},
		{"underscore allowed", "my_service", true},
		{"dash allowed", "my-service", true},
		{"digits allowed", "service123", true},
		{"mixed valid", "web.api_v2-prod", true},
		{"uppercase", "WebAPI", true},
		{"unicode letter", "café", false},
		{"space invalid", "web api", false},
		{"semicolon invalid", "web;rm", false},
		{"slash invalid", "foo/bar", false},
		{"colon invalid", "host:port", false},
		{"at-sign invalid", "user@host", false},
		{"null byte invalid", "web\x00", false},
		{"high unicode codepoint", "αβγ", false},
		// Ensure the unicode.IsLetter-leaking variant is rejected:
		// Latin Extended-A letters are valid Unicode letters but outside ASCII range.
		{"latin extended letter", "façade", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fleet.IsValidFilter(tc.input)
			if got != tc.want {
				t.Errorf("IsValidFilter(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestMatchesFilter(t *testing.T) {
	img := fleet.RuntimeImage{
		Container: "myapp_web_1",
		Service:   "web",
	}
	tests := []struct {
		name    string
		service string
		img     fleet.RuntimeImage
		want    bool
	}{
		{"empty filter matches anything", "", img, true},
		{"exact container name match", "myapp_web_1", img, true},
		{"compose service label match", "web", img, true},
		{"no match", "redis", img, false},
		{"case sensitive no match", "Web", img, false},
		{"container name substring no match", "web_1", img, false},
		{
			"no service label, match by container",
			"standalone",
			fleet.RuntimeImage{Container: "standalone", Service: ""},
			true,
		},
		{
			"no service label, no container match",
			"other",
			fleet.RuntimeImage{Container: "standalone", Service: ""},
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fleet.MatchesFilter(tc.service, tc.img)
			if got != tc.want {
				t.Errorf("MatchesFilter(%q, {Container:%q,Service:%q}) = %v, want %v",
					tc.service, tc.img.Container, tc.img.Service, got, tc.want)
			}
		})
	}
}
