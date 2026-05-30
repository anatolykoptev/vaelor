package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
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
	chat := filepath.Join(parent, "acme-web")
	edge := filepath.Join(parent, "acme-edge")
	for _, d := range []string{chat, edge} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		exec.Command("git", "-C", d, "init").Run()       //nolint:errcheck
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
		Repos: "acme-*", WindowHours: 24, MinPairs: 2,
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
	if !strings.Contains(body, "acme-web") || !strings.Contains(body, "acme-edge") {
		t.Fatalf("pair must name both repos, body=%s", body)
	}
}

func TestFederatedCoChange_EmptyResultIsArrayNotNull(t *testing.T) {
	parent := t.TempDir()
	chat := filepath.Join(parent, "acme-web")
	edge := filepath.Join(parent, "acme-edge")
	for _, d := range []string{chat, edge} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		exec.Command("git", "-C", d, "init").Run()                              //nolint:errcheck
		exec.Command("git", "-C", d, "config", "user.email", "t@t.t").Run()    //nolint:errcheck
		exec.Command("git", "-C", d, "config", "user.name", "t").Run()         //nolint:errcheck
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
		Repos: "acme-*", WindowHours: 24, MinPairs: 2,
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
