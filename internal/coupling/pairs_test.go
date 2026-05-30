package coupling

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-code/internal/federate"
)

func TestVerifyPairs_VerifiedFloatsAboveUnverified(t *testing.T) {
	rootChat := t.TempDir()
	rootEdge := t.TempDir()
	// Real route link: edge server route ↔ chat client call, POST /api/partner/register.
	writeFile(t, rootEdge, "api/register.go", `package api

func init() {
	r.Post("/api/partner/register", handleRegister)
}

func handleRegister(w http.ResponseWriter, r *http.Request) {}
`)
	writeFile(t, rootChat, "src/register.ts", `import axios from "axios";

export async function registerPartner(data: unknown) {
	return axios.post("/api/partner/register", data);
}
`)
	// Noise pair: CHANGELOG ↔ a TS file with no matching route.
	writeFile(t, rootEdge, "CHANGELOG.md", "# changes")
	writeFile(t, rootChat, "src/other.ts", "console.log(\"hi\")")

	roots := map[string]string{"oxpulse-chat": rootChat, "oxpulse-partner-edge": rootEdge}
	// NOISE pair has the HIGHER temporal Score — proves verification re-ranks above raw score.
	cands := []federate.CrossPair{
		{RepoA: "oxpulse-partner-edge", FileA: "CHANGELOG.md", RepoB: "oxpulse-chat", FileB: "src/other.ts", Score: 0.9, CoChanges: 9},
		{RepoA: "oxpulse-partner-edge", FileA: "api/register.go", RepoB: "oxpulse-chat", FileB: "src/register.ts", Score: 0.4, CoChanges: 4},
	}

	out := VerifyPairs(context.Background(), cands, roots, NewRouteVerifier())
	if len(out) != 2 {
		t.Fatalf("want 2 pairs, got %d", len(out))
	}
	if !out[0].Verified {
		t.Fatalf("verified pair must sort first despite lower Score, got %+v", out[0])
	}
	if out[0].FileA != "api/register.go" && out[0].FileB != "api/register.go" {
		t.Fatalf("top pair must be the route-linked one, got %+v", out[0])
	}
	if out[0].LinkedBy == "" {
		t.Fatalf("verified pair must have a LinkedBy label")
	}
	if out[1].Verified {
		t.Fatalf("CHANGELOG pair must be unverified, got %+v", out[1])
	}
}

func TestVerifyPairs_MissingRootUnverified(t *testing.T) {
	// A pair whose repo slug isn't in roots cannot be read → unverified, not a panic.
	cands := []federate.CrossPair{
		{RepoA: "unknown", FileA: "a.go", RepoB: "other", FileB: "b.ts", Score: 0.5},
	}
	out := VerifyPairs(context.Background(), cands, map[string]string{}, NewRouteVerifier())
	if len(out) != 1 || out[0].Verified {
		t.Fatalf("missing-root pair must be returned unverified, got %+v", out)
	}
}
