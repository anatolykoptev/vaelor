package embeddings

import "testing"

func TestParseSignature(t *testing.T) {
	tests := []struct {
		name         string
		sig          string
		wantIsMethod bool
		wantReceiver string
		wantNorm     string
	}{
		{
			name:         "pointer receiver with ident",
			sig:          "func (g *GitHubForge) FetchREADME(ctx context.Context, slug string) (_ string, err error)",
			wantIsMethod: true,
			wantReceiver: "GitHubForge",
			wantNorm:     "func FetchREADME(ctx context.Context, slug string) (_ string, err error)",
		},
		{
			name:         "value receiver with ident",
			sig:          "func (s Store) Get(k string) string",
			wantIsMethod: true,
			wantReceiver: "Store",
			wantNorm:     "func Get(k string) string",
		},
		{
			name:         "pointer receiver no ident",
			sig:          "func (*Store) Get(k string) string",
			wantIsMethod: true,
			wantReceiver: "Store",
			wantNorm:     "func Get(k string) string",
		},
		{
			name:         "generic receiver",
			sig:          "func (c *Cache[K, V]) Get(k K) V",
			wantIsMethod: true,
			wantReceiver: "Cache",
			wantNorm:     "func Get(k K) V",
		},
		{
			name:         "free function",
			sig:          "func countSourceFiles(files []*ingest.File) int",
			wantIsMethod: false,
		},
		{
			name:         "free function no params",
			sig:          "func commonPrefixLen() int",
			wantIsMethod: false,
		},
		{
			name:         "empty",
			sig:          "",
			wantIsMethod: false,
		},
		{
			name:         "non-go gibberish",
			sig:          "def foo(self):",
			wantIsMethod: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSignature(tt.sig)
			if got.isMethod != tt.wantIsMethod {
				t.Fatalf("isMethod = %v, want %v (sig=%q)", got.isMethod, tt.wantIsMethod, tt.sig)
			}
			if !tt.wantIsMethod {
				return
			}
			if got.receiver != tt.wantReceiver {
				t.Errorf("receiver = %q, want %q", got.receiver, tt.wantReceiver)
			}
			if got.norm != tt.wantNorm {
				t.Errorf("norm = %q, want %q", got.norm, tt.wantNorm)
			}
		})
	}
}

func TestIsInterfaceSiblingPair(t *testing.T) {
	// The FetchREADME false-positive: two methods, same name, identical
	// receiver-stripped signature, DISTINCT receiver types → interface siblings.
	gitHub := parseSignature("func (g *GitHubForge) FetchREADME(ctx context.Context, slug string) (_ string, err error)")
	gitLab := parseSignature("func (g *GitLabForge) FetchREADME(ctx context.Context, slug string) (_ string, err error)")
	if !isInterfaceSiblingPair("FetchREADME", gitHub, "FetchREADME", gitLab) {
		t.Error("FetchREADME pair (distinct receivers, same sig) should be an interface sibling")
	}

	// The countSourceFiles true-positive: two free functions, same name, no
	// receiver → NOT interface siblings (must be kept as a genuine duplicate).
	cf1 := parseSignature("func countSourceFiles(files []*ingest.File) int")
	cf2 := parseSignature("func countSourceFiles(files []*ingest.File) int")
	if isInterfaceSiblingPair("countSourceFiles", cf1, "countSourceFiles", cf2) {
		t.Error("countSourceFiles pair (free functions) must NOT be flagged as interface siblings — over-suppression")
	}

	// Same receiver type, same name → not distinct receivers → not siblings.
	sameRecvA := parseSignature("func (g *GitHubForge) FetchREADME(ctx context.Context, slug string) (_ string, err error)")
	sameRecvB := parseSignature("func (g *GitHubForge) FetchREADME(ctx context.Context, slug string) (_ string, err error)")
	if isInterfaceSiblingPair("FetchREADME", sameRecvA, "FetchREADME", sameRecvB) {
		t.Error("same receiver type must not be flagged as siblings (would need distinct types)")
	}

	// Different method names → not siblings even if both methods on distinct types.
	if isInterfaceSiblingPair("FetchREADME", gitHub, "FetchTags", gitLab) {
		t.Error("different method names must not be flagged as siblings")
	}

	// Distinct receivers but DIFFERENT normalized signature (different params) →
	// not the same interface method → keep. This is a genuine could-be-dup that
	// the conservative discriminator must not suppress.
	wideParams := parseSignature("func (g *GitLabForge) FetchREADME(ctx context.Context, slug string, ref string) (_ string, err error)")
	if isInterfaceSiblingPair("FetchREADME", gitHub, "FetchREADME", wideParams) {
		t.Error("distinct receivers with different signatures must NOT be flagged as siblings")
	}

	// Method ↔ free function → not siblings (one side has no receiver).
	if isInterfaceSiblingPair("FetchREADME", gitHub, "FetchREADME", cf1) {
		t.Error("method paired with free function must not be flagged as siblings")
	}
}
