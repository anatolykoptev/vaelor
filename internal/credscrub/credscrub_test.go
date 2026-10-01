package credscrub

import (
	"strings"
	"testing"
)

func TestScrub(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"clean", "fatal: repository not found", "fatal: repository not found"},
		{"url token userinfo",
			"fatal: could not read Password for 'https://SENTINELTOK@github.com': terminal prompts disabled",
			"fatal: could not read Password for 'https://***@github.com': terminal prompts disabled"},
		{"url user:pass", "clone https://user:pass@host.example/o/r.git failed", "clone https://***@host.example/o/r.git failed"},
		{"ssh scheme", "ssh://git:secret@host/x", "ssh://***@host/x"},
		{"scp-like form untouched", "git@github.com:o/r.git", "git@github.com:o/r.git"},
		{"ghs", "token ghs_AbC123_xyz leaked", "token *** leaked"},
		{"ghp gho ghu ghr", "ghp_a gho_b ghu_c ghr_d", "*** *** *** ***"},
		{"fine-grained", "github_pat_11AAA_bbbCCC end", "*** end"},
		{"gitlab", "glpat-abc-DEF_123 end", "*** end"},
		{"auth header", "Authorization: Basic eHRyYTpzZWNyZXQ=", "Authorization: Basic ***"},
		{"bearer ci", "authorization: bearer abc.def", "authorization: bearer ***"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Scrub(tc.in); got != tc.want {
				t.Errorf("Scrub(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestScrub_NoSecretSurvives(t *testing.T) {
	t.Parallel()
	in := "a ghs_SECRETONE b https://u:SECRETTWO@h/x c glpat-SECRETTHREE d github_pat_SECRETFOUR"
	out := Scrub(in)
	for _, s := range []string{"SECRETONE", "SECRETTWO", "SECRETTHREE", "SECRETFOUR"} {
		if strings.Contains(out, s) {
			t.Errorf("%s survived: %q", s, out)
		}
	}
}
