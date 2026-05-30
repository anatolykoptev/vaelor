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
	chat := filepath.Join(parent, "oxpulse-chat")
	edge := filepath.Join(parent, "oxpulse-partner-edge")
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
}
