package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/coupling"
)

func TestFederatedCoChange_RequiresRepos(t *testing.T) {
	res, err := handleFederatedCoChangeCore(context.Background(), FederatedCoChangeArgs{Repos: ""}, analyze.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("empty repos must be an error")
	}
}

func TestFederatedCoChange_FindsCrossRepoPair(t *testing.T) {
	parent := t.TempDir()
	chat := filepath.Join(parent, "oxpulse-chat")
	edge := filepath.Join(parent, "oxpulse-partner-edge")
	for _, d := range []string{chat, edge} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		exec.Command("git", "-C", d, "init").Run()                          //nolint:errcheck
		exec.Command("git", "-C", d, "config", "user.email", "t@t.t").Run() //nolint:errcheck
		exec.Command("git", "-C", d, "config", "user.name", "t").Run()      //nolint:errcheck
	}
	commit := func(dir, file, date string) {
		os.WriteFile(filepath.Join(dir, file), []byte(date+"\n"), 0o644) //nolint:errcheck
		exec.Command("git", "-C", dir, "add", file).Run()                //nolint:errcheck
		c := exec.Command("git", "-C", dir, "commit", "-m", "x")
		c.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		c.Run() //nolint:errcheck
	}
	for _, date := range []string{"2026-05-01T10:00:00+00:00", "2026-05-08T10:00:00+00:00"} {
		commit(chat, "rooms.rs", date)
		commit(edge, "install.sh", date)
	}
	// Background commits so rooms.rs/install.sh appear in 2 of 4 windows (50% < 85%
	// ubiquity threshold) — without these, 2/2=100% would trigger the stop-word filter.
	commit(chat, "bg.go", "2026-05-15T10:00:00+00:00")
	commit(edge, "bg.sh", "2026-05-22T10:00:00+00:00")

	deps := analyze.Deps{LocalRepoDirs: []string{parent}}
	res, err := handleFederatedCoChangeCore(context.Background(), FederatedCoChangeArgs{
		Repos: "oxpulse-*", WindowHours: 24, MinPairs: 2,
	}, deps)
	if err != nil || res.IsError {
		t.Fatalf("unexpected: err=%v isErr=%v", err, res.IsError)
	}
	body := extractText(t, res)
	var out FederatedCoChangeResult
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("parse: %v\nbody=%s", err, body)
	}
	if len(out.Pairs) == 0 {
		t.Fatalf("expected cross-repo pairs, body=%s", body)
	}
	if !strings.Contains(body, "oxpulse-chat") || !strings.Contains(body, "oxpulse-partner-edge") {
		t.Fatalf("pair must name both repos, body=%s", body)
	}
	// Pairs are now VerifiedPair (stage-2 output). The synthetic git fixtures have no real
	// route files, so verified=false is expected — but the field must be present in the JSON.
	_ = coupling.VerifiedPair{} // compile-time import check
	if !strings.Contains(body, `"verified"`) {
		t.Fatalf("VerifiedPair output must include verified field, body=%s", body)
	}
}

func TestFederatedCoChange_SymbolVerifiesProtocolToken(t *testing.T) {
	parent := t.TempDir()
	chat := filepath.Join(parent, "oxpulse-chat")
	edge := filepath.Join(parent, "oxpulse-partner-edge")
	for _, d := range []string{chat, edge} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		exec.Command("git", "-C", d, "init").Run()                          //nolint:errcheck
		exec.Command("git", "-C", d, "config", "user.email", "t@t.t").Run() //nolint:errcheck
		exec.Command("git", "-C", d, "config", "user.name", "t").Run()      //nolint:errcheck
	}
	commitContent := func(dir, file, content, date string) {
		os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644) //nolint:errcheck
		exec.Command("git", "-C", dir, "add", file).Run()              //nolint:errcheck
		// --no-verify: these are isolated fixture repos in t.TempDir() — the global
		// gitleaks hook would block RELAY_JWT_SECRET content, defeating the test's purpose.
		c := exec.Command("git", "-C", dir, "commit", "--no-verify", "-m", "x")
		c.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		c.Run() //nolint:errcheck
	}
	// Each co-change iteration writes slightly different content (appends a revision
	// comment) so git registers a real change and doesn't skip the commit.
	// Both versions contain RELAY_JWT_SECRET and "peer_joined" — the two shared tokens
	// that symbol verification must find.
	for i, date := range []string{"2026-05-01T10:00:00+00:00", "2026-05-08T10:00:00+00:00"} {
		chatSrc := fmt.Sprintf(`const secret = import.meta.env.RELAY_JWT_SECRET;
socket.on("peer_joined", () => {}); // rev %d`, i)
		edgeSrc := fmt.Sprintf(`let secret = std::env::var("RELAY_JWT_SECRET").unwrap();
match m { "peer_joined" => fanout(), _ => {} } // rev %d`, i)
		commitContent(chat, "signal.ts", chatSrc, date)
		commitContent(edge, "fanout.rs", edgeSrc, date)
	}
	// Background commits so the protocol files appear in 2 of 4 windows (<85% ubiquity).
	commitContent(chat, "bg.go", "package main", "2026-05-15T10:00:00+00:00")
	commitContent(edge, "bg.rs", "fn bg() {}", "2026-05-22T10:00:00+00:00")

	deps := analyze.Deps{LocalRepoDirs: []string{parent}}
	res, err := handleFederatedCoChangeCore(context.Background(), FederatedCoChangeArgs{
		Repos: "oxpulse-*", WindowHours: 24, MinPairs: 2,
	}, deps)
	if err != nil || res.IsError {
		t.Fatalf("unexpected: err=%v isErr=%v", err, res.IsError)
	}
	body := extractText(t, res)
	var out FederatedCoChangeResult
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("parse: %v\nbody=%s", err, body)
	}
	// Find the signal.ts <-> fanout.rs pair and assert it is symbol-verified.
	var found bool
	for _, p := range out.Pairs {
		if !p.Verified {
			continue
		}
		for _, e := range p.Evidence {
			if e.Kind == "symbol" && (e.Detail == "RELAY_JWT_SECRET" || e.Detail == "peer_joined") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected a symbol-verified pair on RELAY_JWT_SECRET/peer_joined, body=%s", body)
	}
}

func TestFederatedCoChange_EmptyResultIsArrayNotNull(t *testing.T) {
	parent := t.TempDir()
	chat := filepath.Join(parent, "oxpulse-chat")
	edge := filepath.Join(parent, "oxpulse-partner-edge")
	for _, d := range []string{chat, edge} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		exec.Command("git", "-C", d, "init").Run()                          //nolint:errcheck
		exec.Command("git", "-C", d, "config", "user.email", "t@t.t").Run() //nolint:errcheck
		exec.Command("git", "-C", d, "config", "user.name", "t").Run()      //nolint:errcheck
	}
	commit := func(dir, file, date string) {
		os.WriteFile(filepath.Join(dir, file), []byte(date+"\n"), 0o644) //nolint:errcheck
		exec.Command("git", "-C", dir, "add", file).Run()                //nolint:errcheck
		c := exec.Command("git", "-C", dir, "commit", "-m", "x")
		c.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		c.Run() //nolint:errcheck
	}
	// Commits far apart in time → no shared window → zero cross-repo pairs.
	commit(chat, "a.rs", "2026-01-01T10:00:00+00:00")
	commit(edge, "b.sh", "2026-05-01T10:00:00+00:00")

	deps := analyze.Deps{LocalRepoDirs: []string{parent}}
	res, err := handleFederatedCoChangeCore(context.Background(), FederatedCoChangeArgs{
		Repos: "oxpulse-*", WindowHours: 24, MinPairs: 2,
	}, deps)
	if err != nil || res.IsError {
		t.Fatalf("unexpected: err=%v isErr=%v", err, res.IsError)
	}
	body := extractText(t, res)
	// The wire contract must always be an array, never null —
	// MCP consumers (JS/Python) do `for (const p of result.pairs)` which throws on null.
	if strings.Contains(body, `"pairs": null`) {
		t.Fatalf("empty result must serialize pairs as [], got null; body=%s", body)
	}
	if !strings.Contains(body, `"pairs": []`) {
		t.Fatalf("empty result must serialize pairs as [], body=%s", body)
	}
}
