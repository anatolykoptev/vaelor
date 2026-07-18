//go:build smoke

// Package fleet_test provides a build-tagged smoke test for the fleet subsystem.
//
// This file is an operator smoke harness — it runs against a real docker.sock
// (local probe) or a live SSH host (remote probe) and is NOT part of the normal
// `go test ./...` suite. It is intentionally excluded from CI to avoid requiring
// real infrastructure.
//
// Usage:
//
//	GOWORK=off go test -tags=smoke -run TestSmoke -v -count=1 ./internal/fleet/
//
// Environment variables:
//
//	SMOKE_REPO   path to a krolik-server checkout (default: ~/deploy/krolik-server)
//	SMOKE_HOST   target URL: "local://" for local docker.sock, "ssh://user@host"
//	             for a remote host via the SSH driver (default: local://)
//
// The test collects pinned images from Dockerfiles/compose files in SMOKE_REPO,
// lists running containers from SMOKE_HOST, and logs the diff (Match / Drifted /
// Unmatched). No assertions are made — this is a diagnostic, not a correctness check.
package fleet_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/fleet"
	"github.com/anatolykoptev/vaelor/internal/fleet/docker"
	"github.com/anatolykoptev/vaelor/internal/fleet/ssh"
	"github.com/anatolykoptev/vaelor/internal/polyglot/pinned"
)

// Run with:
//
//	GOWORK=off go test -tags=smoke -run TestSmoke -v -count=1 //     -repo=/path -host=local:// ./internal/fleet/
//
// Reads SMOKE_REPO env (default krolik-server) and SMOKE_HOST env (default local://).
func TestSmoke(t *testing.T) {
	t.Parallel()
	repo := os.Getenv("SMOKE_REPO")
	if repo == "" {
		repo = "/home/krolik/deploy/krolik-server"
	}
	hostArg := os.Getenv("SMOKE_HOST")
	if hostArg == "" {
		hostArg = "local://"
	}
	t.Logf("repo=%s host=%s", repo, hostArg)

	pin, err := pinned.Collect(repo)
	if err != nil {
		t.Logf("pinned.Collect: %v (continuing)", err)
	}
	t.Logf("pinned: %d images from compose+Dockerfile", len(pin))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var rt []fleet.RuntimeImage
	target, err := fleet.ParseTarget(hostArg)
	if err != nil {
		t.Fatalf("ParseTarget: %v", err)
	}

	switch target.Scheme {
	case "local", "docker":
		target.Scheme = "docker"
		drv := docker.New(docker.WithTimeout(5 * time.Second))
		rt, err = drv.List(ctx, target, fleet.Filter{})
	case "ssh":
		drv := ssh.New(ssh.WithEnabled(true), ssh.WithTimeout(8*time.Second))
		rt, err = drv.List(ctx, target, fleet.Filter{})
	default:
		t.Fatalf("unsupported scheme: %s", target.Scheme)
	}
	if err != nil {
		t.Fatalf("probe error: %v", err)
	}
	t.Logf("runtime: %d containers", len(rt))

	diffs := fleet.Diff(pin, rt)
	t.Logf("diffs: %d rows total", len(diffs))

	bystatus := map[fleet.DiffStatus]int{}
	for _, d := range diffs {
		bystatus[d.Status]++
	}
	for s, n := range bystatus {
		t.Logf("  %-15s %d", s, n)
	}

	// Print non-Match rows for inspection.
	var interesting []fleet.ImageDiff
	for _, d := range diffs {
		if d.Status != fleet.DiffMatch {
			interesting = append(interesting, d)
		}
	}
	if len(interesting) == 0 {
		t.Log("\n(no non-Match diffs — full alignment)")
		return
	}
	out, _ := json.MarshalIndent(interesting, "", "  ")
	fmt.Println(string(out))
}
