package coupling

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Go server: chi-style r.Post("/api/partner/register", handleRegister)
// TS client: axios.post("/api/partner/register") — same method+path
func TestRouteVerifier_ServerClientMatch(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()

	// Go server route recognized by routerMethodRe (chi pattern):
	//   \w+\.(Post)\("/api/partner/register", handlerName)
	writeFile(t, rootA, "api/handler.go", `package api

func init() {
	r.Post("/api/partner/register", handleRegister)
}

func handleRegister(w http.ResponseWriter, r *http.Request) {}
`)

	// TS client route recognized by tsAxiosRe:
	//   axios.post("/api/partner/register")
	writeFile(t, rootB, "src/client.ts", `import axios from "axios";

export async function registerPartner(data: unknown) {
	return axios.post("/api/partner/register", data);
}
`)

	v := NewRouteVerifier()
	ev, err := v.Verify(context.Background(),
		FilePair{Repo: "repoA", Root: rootA, Rel: "api/handler.go"},
		FilePair{Repo: "repoB", Root: rootB, Rel: "src/client.ts"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) == 0 {
		t.Fatalf("expected route evidence for matching server/client route")
	}
	if ev[0].Kind != "route" || ev[0].Tier != "offline" || ev[0].Detail == "" {
		t.Fatalf("evidence wrong: %+v", ev[0])
	}
}

func TestRouteVerifier_NoMatchForMarkdown(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()

	writeFile(t, rootA, "CHANGELOG.md", "# Changelog\n- did stuff\n")
	// TS client route — valid, but markdown on the other side has no routes.
	writeFile(t, rootB, "src/client.ts", `import axios from "axios";
export const ping = () => axios.get("/api/health");
`)

	v := NewRouteVerifier()
	ev, err := v.Verify(context.Background(),
		FilePair{Repo: "a", Root: rootA, Rel: "CHANGELOG.md"},
		FilePair{Repo: "b", Root: rootB, Rel: "src/client.ts"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 0 {
		t.Fatalf("markdown defines no route → no evidence, got %+v", ev)
	}
}

// Go server on /api/a, TS client on /api/b — paths differ, no match expected.
func TestRouteVerifier_NoMatchWhenPathsDiffer(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()

	writeFile(t, rootA, "h.go", `package api
func init() { r.Post("/api/a", handlerA) }
func handlerA(w http.ResponseWriter, r *http.Request) {}
`)
	writeFile(t, rootB, "c.ts", `import axios from "axios";
export const callB = () => axios.post("/api/b", null);
`)

	v := NewRouteVerifier()
	ev, _ := v.Verify(context.Background(),
		FilePair{Repo: "a", Root: rootA, Rel: "h.go"},
		FilePair{Repo: "b", Root: rootB, Rel: "c.ts"})
	if len(ev) != 0 {
		t.Fatalf("different paths must not match, got %+v", ev)
	}
}

// TestRouteVerifier_SkipsGenericPath — two unrelated repos both expose GET /health
// (a path collision, not a real cross-repo dependency). The verifier must not emit
// evidence for well-known generic endpoints.
func TestRouteVerifier_SkipsGenericPath(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()

	writeFile(t, rootA, "h.go", `package api
func init() { r.Get("/health", handleHealth) }
func handleHealth(w http.ResponseWriter, r *http.Request) {}
`)
	writeFile(t, rootB, "c.ts", `import axios from "axios";
export const checkHealth = () => axios.get("/health");
`)

	v := NewRouteVerifier()
	ev, _ := v.Verify(context.Background(),
		FilePair{Repo: "a", Root: rootA, Rel: "h.go"},
		FilePair{Repo: "b", Root: rootB, Rel: "c.ts"})
	if len(ev) != 0 {
		t.Fatalf("generic /health path must NOT count as evidence, got %+v", ev)
	}
}

// TestRouteVerifier_SpecificPathStillMatches — a 2+ segment specific path
// IS a real cross-repo dependency and must still produce evidence.
func TestRouteVerifier_SpecificPathStillMatches(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()

	writeFile(t, rootA, "h.go", `package api
func init() { r.Post("/api/partner/register", handleRegister) }
func handleRegister(w http.ResponseWriter, r *http.Request) {}
`)
	writeFile(t, rootB, "c.ts", `import axios from "axios";
export async function registerPartner(data: unknown) {
	return axios.post("/api/partner/register", data);
}
`)

	v := NewRouteVerifier()
	ev, _ := v.Verify(context.Background(),
		FilePair{Repo: "a", Root: rootA, Rel: "h.go"},
		FilePair{Repo: "b", Root: rootB, Rel: "c.ts"})
	if len(ev) == 0 {
		t.Fatalf("specific /api/partner/register must still match")
	}
}
