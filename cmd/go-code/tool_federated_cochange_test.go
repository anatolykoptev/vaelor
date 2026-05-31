package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/coupling"
	"github.com/anatolykoptev/go-code/internal/federate"
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
		exec.Command("git", "-C", d, "init").Run()                          //nolint:errcheck,noctx // test fixture: no timeout needed for git init
		exec.Command("git", "-C", d, "config", "user.email", "t@t.t").Run() //nolint:errcheck,noctx // test fixture: git config
		exec.Command("git", "-C", d, "config", "user.name", "t").Run()      //nolint:errcheck,noctx // test fixture: git config
	}
	commit := func(dir, file, date string) {
		os.WriteFile(filepath.Join(dir, file), []byte(date+"\n"), 0o644) //nolint:errcheck
		exec.Command("git", "-C", dir, "add", file).Run()                //nolint:errcheck,noctx // test fixture: git add
		c := exec.Command("git", "-C", dir, "commit", "-m", "x")         //nolint:noctx // test fixture: git commit
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
	// Pairs are now VerifiedPair (stage-2 output). The synthetic git fixtures have no real
	// route files, so verified=false is expected — but the field must be present in the JSON.
	_ = coupling.VerifiedPair{} // compile-time import check
	if !strings.Contains(body, `"verified"`) {
		t.Fatalf("VerifiedPair output must include verified field, body=%s", body)
	}
}

func TestFederatedCoChange_SymbolVerifiesProtocolToken(t *testing.T) {
	parent := t.TempDir()
	chat := filepath.Join(parent, "acme-web")
	edge := filepath.Join(parent, "acme-edge")
	for _, d := range []string{chat, edge} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		exec.Command("git", "-C", d, "init").Run()                          //nolint:errcheck,noctx // test fixture: git init
		exec.Command("git", "-C", d, "config", "user.email", "t@t.t").Run() //nolint:errcheck,noctx // test fixture: git config
		exec.Command("git", "-C", d, "config", "user.name", "t").Run()      //nolint:errcheck,noctx // test fixture: git config
	}
	commitContent := func(dir, file, content, date string) {
		os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644) //nolint:errcheck
		exec.Command("git", "-C", dir, "add", file).Run()              //nolint:errcheck,noctx // test fixture: git add
		// --no-verify: these are isolated fixture repos in t.TempDir() — the global
		// gitleaks hook would block RELAY_JWT_SECRET content, defeating the test's purpose.
		c := exec.Command("git", "-C", dir, "commit", "--no-verify", "-m", "x") //nolint:noctx // test fixture: git commit
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
	chat := filepath.Join(parent, "acme-web")
	edge := filepath.Join(parent, "acme-edge")
	for _, d := range []string{chat, edge} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		exec.Command("git", "-C", d, "init").Run()                          //nolint:errcheck,noctx // test fixture: git init
		exec.Command("git", "-C", d, "config", "user.email", "t@t.t").Run() //nolint:errcheck,noctx // test fixture: git config
		exec.Command("git", "-C", d, "config", "user.name", "t").Run()      //nolint:errcheck,noctx // test fixture: git config
	}
	commit := func(dir, file, date string) {
		os.WriteFile(filepath.Join(dir, file), []byte(date+"\n"), 0o644) //nolint:errcheck
		exec.Command("git", "-C", dir, "add", file).Run()                //nolint:errcheck,noctx // test fixture: git add
		c := exec.Command("git", "-C", dir, "commit", "-m", "x")         //nolint:noctx // test fixture: git commit
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

// makeCoChangeTempRepos creates two git repos under a temp dir with coordinated
// commits (same-day) so cross-repo pairs can be detected, plus background commits
// to stay below the 85% ubiquity threshold.
func makeCoChangeTempRepos(t *testing.T) (parent, chatDir, edgeDir string) {
	t.Helper()
	parent = t.TempDir()
	chatDir = filepath.Join(parent, "acme-web")
	edgeDir = filepath.Join(parent, "acme-edge")
	for _, d := range []string{chatDir, edgeDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		exec.Command("git", "-C", d, "init").Run()                          //nolint:errcheck,noctx // test fixture: git init
		exec.Command("git", "-C", d, "config", "user.email", "t@t.t").Run() //nolint:errcheck,noctx // test fixture: git config
		exec.Command("git", "-C", d, "config", "user.name", "t").Run()      //nolint:errcheck,noctx // test fixture: git config
	}
	commit := func(dir, file, date string) {
		os.WriteFile(filepath.Join(dir, file), []byte(date+"\n"), 0o644) //nolint:errcheck
		exec.Command("git", "-C", dir, "add", file).Run()                //nolint:errcheck,noctx // test fixture: git add
		c := exec.Command("git", "-C", dir, "commit", "-m", "x")         //nolint:noctx // test fixture: git commit
		c.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		c.Run() //nolint:errcheck
	}
	for _, date := range []string{"2026-05-01T10:00:00+00:00", "2026-05-08T10:00:00+00:00"} {
		commit(chatDir, "rooms.rs", date)
		commit(edgeDir, "install.sh", date)
	}
	commit(chatDir, "bg.go", "2026-05-15T10:00:00+00:00")
	commit(edgeDir, "bg.sh", "2026-05-22T10:00:00+00:00")
	return
}

// TestFederatedCoChange_DeadlineHit_ReturnsPartialOrBuilding verifies that when the
// inline budget is exhausted the handler returns a non-error response with
// status "partial" or "building" and retry_after_seconds > 0, never a bare timeout.
func TestFederatedCoChange_DeadlineHit_ReturnsPartialOrBuilding(t *testing.T) {
	parent, _, _ := makeCoChangeTempRepos(t)

	// Clean the cache for this test so repos start cold.
	federatedCoChangeCache.Range(func(k, _ any) bool { federatedCoChangeCache.Delete(k); return true })
	fedInFlight.Range(func(k, _ any) bool { fedInFlight.Delete(k); return true })

	deps := analyze.Deps{LocalRepoDirs: []string{parent}}
	// Use a 1ns budget — guaranteed to expire before any git log completes.
	res, err := handleFederatedCoChangeCoreWithBudget(context.Background(), FederatedCoChangeArgs{
		Repos: "acme-*", WindowHours: 24, MinPairs: 2,
	}, deps, time.Nanosecond)

	if err != nil {
		t.Fatalf("handler must not return Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handler must not return MCP error on deadline; body=%s", extractText(t, res))
	}
	body := extractText(t, res)
	var out FederatedCoChangeResult
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("response must be valid JSON: %v\nbody=%s", err, body)
	}
	if out.Status != fedStatusPartial && out.Status != fedStatusBuilding {
		t.Fatalf("expected status partial|building, got %q; body=%s", out.Status, body)
	}
	if out.RetryAfterSeconds <= 0 {
		t.Fatalf("expected retry_after_seconds>0, got %d; body=%s", out.RetryAfterSeconds, body)
	}
	// pairs must always be an array, never null.
	if strings.Contains(body, `"pairs": null`) {
		t.Fatalf("pairs must be [] not null on partial; body=%s", body)
	}
}

// TestFederatedCoChange_WarmCacheGivesPartialPairs verifies that pre-warming the
// touches cache (by letting a full-budget call populate it) causes a subsequent
// tight-budget call to return warm pairs immediately.
func TestFederatedCoChange_WarmCacheGivesPartialPairs(t *testing.T) {
	parent, chatDir, edgeDir := makeCoChangeTempRepos(t)

	// Clean state.
	federatedCoChangeCache.Range(func(k, _ any) bool { federatedCoChangeCache.Delete(k); return true })
	fedInFlight.Range(func(k, _ any) bool { fedInFlight.Delete(k); return true })

	deps := analyze.Deps{LocalRepoDirs: []string{parent}}

	// Warm the cache with a full-budget call.
	_, _ = handleFederatedCoChangeCoreWithBudget(context.Background(), FederatedCoChangeArgs{
		Repos: "acme-*", WindowHours: 24, MinPairs: 2,
	}, deps, 60*time.Second)

	// Both repos must be warm for the status assertion below to be valid.
	// Skip only when BOTH repos failed to warm — a single-repo skip would make
	// the status assertion vacuous (one warm repo → "building", not "partial").
	if !federate.IsRepoWarm(chatDir) || !federate.IsRepoWarm(edgeDir) {
		t.Skip("touches cache not warm for both repos after 60s — skip in slow CI")
	}
	if federate.WarmTouches(chatDir) == nil {
		t.Fatal("WarmTouches returned nil but IsRepoWarm true — inconsistency")
	}

	// Drop result cache only, keeping touches cache warm.
	federatedCoChangeCache.Range(func(k, _ any) bool { federatedCoChangeCache.Delete(k); return true })
	fedInFlight.Range(func(k, _ any) bool { fedInFlight.Delete(k); return true })

	// Now call with tiny budget — touches are warm so we should get a partial/ready result.
	res, err := handleFederatedCoChangeCoreWithBudget(context.Background(), FederatedCoChangeArgs{
		Repos: "acme-*", WindowHours: 24, MinPairs: 2,
	}, deps, time.Nanosecond)

	if err != nil || res.IsError {
		t.Fatalf("unexpected error on warm call: err=%v isErr=%v body=%s", err, res.IsError, extractText(t, res))
	}
	body := extractText(t, res)
	if strings.Contains(body, `"pairs": null`) {
		t.Fatalf("pairs must never be null; body=%s", body)
	}
	var out FederatedCoChangeResult
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("response must be valid JSON: %v\nbody=%s", err, body)
	}
	// Both repos were warm (skip guard above). With ≥2 warm repos the handler
	// must return "partial" or "ready" — not "building" (building = <2 warm repos).
	if out.Status != fedStatusPartial && out.Status != fedStatusReady && out.Status != "" {
		t.Fatalf("expected status partial|ready (both repos warm), got %q; body=%s", out.Status, body)
	}
}

// TestFederatedCoChange_PollReturnsReady verifies that after a background job
// stores its result, a second call with the same args hits the cache and returns
// status "ready" (empty string in JSON via omitempty) with the full pair set.
func TestFederatedCoChange_PollReturnsReady(t *testing.T) {
	parent, _, _ := makeCoChangeTempRepos(t)

	// Clean state.
	federatedCoChangeCache.Range(func(k, _ any) bool { federatedCoChangeCache.Delete(k); return true })
	fedInFlight.Range(func(k, _ any) bool { fedInFlight.Delete(k); return true })

	deps := analyze.Deps{LocalRepoDirs: []string{parent}}
	args := FederatedCoChangeArgs{Repos: "acme-*", WindowHours: 24, MinPairs: 2}

	// Pre-populate the result cache as if a background job has completed.
	cacheKey := federatedCoChangeCacheKey(args.Repos, federatedCoChangeDefaultWindowHours, federatedCoChangeDefaultMinPairs, 0, deps.LocalRepoDirs)
	fakePairs := []coupling.VerifiedPair{
		{CrossPair: federate.CrossPair{RepoA: "acme-web", FileA: "rooms.rs", RepoB: "acme-edge", FileB: "install.sh", CoChanges: 2}},
	}
	federatedCoChangeCache.Store(cacheKey, &federatedCoChangeCacheEntry{
		result: &FederatedCoChangeResult{Pairs: fakePairs},
		done:   true,
	})

	res, err := handleFederatedCoChangeCore(context.Background(), args, deps)
	if err != nil || res.IsError {
		t.Fatalf("unexpected error: err=%v isErr=%v body=%s", err, res.IsError, extractText(t, res))
	}
	body := extractText(t, res)
	var out FederatedCoChangeResult
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("parse: %v\nbody=%s", err, body)
	}
	// status "ready" is emitted as omitempty empty string — both empty and "ready" are acceptable.
	if out.Status != "" && out.Status != fedStatusReady {
		t.Fatalf("expected ready/empty status on cache hit, got %q; body=%s", out.Status, body)
	}
	if len(out.Pairs) == 0 {
		t.Fatalf("expected cached pairs in response; body=%s", body)
	}
	if out.Pairs[0].RepoA != "acme-web" {
		t.Fatalf("expected cached pair repoA=acme-web, got %q; body=%s", out.Pairs[0].RepoA, body)
	}
	// retry_after_seconds must be 0 (omitted) on ready response.
	if out.RetryAfterSeconds != 0 {
		t.Fatalf("ready response must not have retry_after_seconds, got %d; body=%s", out.RetryAfterSeconds, body)
	}
}

// TestFederatedCoChange_DeduplicatesConcurrentCalls verifies that concurrent calls
// with the same args launch exactly one background worker (fedInFlight LoadOrStore guard).
//
// Design: inject fedBgComputeHook to count actual compute invocations AND block the
// background goroutine until all N callers have raced (using a gate channel).  All N
// calls arrive with a 1ns budget → all hit the background path → only the first
// LoadOrStore succeeds → hook fires exactly once.  Releasing the gate lets the worker
// finish.  The test FAILS if the LoadOrStore guard is removed (hook fires N times).
func TestFederatedCoChange_DeduplicatesConcurrentCalls(t *testing.T) {
	parent, _, _ := makeCoChangeTempRepos(t)

	// Clean state.
	federatedCoChangeCache.Range(func(k, _ any) bool { federatedCoChangeCache.Delete(k); return true })
	fedInFlight.Range(func(k, _ any) bool { fedInFlight.Delete(k); return true })

	// gate blocks the background worker until we release it, ensuring all 10
	// concurrent callers arrive BEFORE the first worker finishes and deletes its
	// fedInFlight entry (which would allow a second worker to be launched).
	gate := make(chan struct{})
	var computeCount atomic.Int64
	fedBgComputeHook = func() {
		computeCount.Add(1)
		<-gate // block until the test releases
	}
	t.Cleanup(func() { fedBgComputeHook = nil })

	deps := analyze.Deps{LocalRepoDirs: []string{parent}}
	args := FederatedCoChangeArgs{Repos: "acme-*", WindowHours: 24, MinPairs: 2}
	budget := time.Nanosecond // force deadline on every call — all go to background path

	const concurrency = 10
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = handleFederatedCoChangeCoreWithBudget(context.Background(), args, deps, budget)
		}()
	}
	wg.Wait() // all callers have returned; background worker is blocked on gate

	// Assert exactly 1 worker was launched while all 10 callers were in-flight.
	got := computeCount.Load()
	if got != 1 {
		close(gate) // release before fatal so cleanup doesn't deadlock
		t.Fatalf("expected exactly 1 background compute, got %d — LoadOrStore dedup guard may be broken", got)
	}

	// Release the gate and let the worker finish.
	close(gate)
}

// TestFederatedCoChange_BackCompatPairsAlwaysArray verifies the back-compat
// guarantee: "pairs" is always a JSON array on all response types (ready/partial/building).
func TestFederatedCoChange_BackCompatPairsAlwaysArray(t *testing.T) {
	parent, _, _ := makeCoChangeTempRepos(t)

	for _, tc := range []struct {
		name   string
		budget time.Duration
	}{
		{"tiny_budget_cold", time.Nanosecond},
		{"full_budget", 60 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			federatedCoChangeCache.Range(func(k, _ any) bool { federatedCoChangeCache.Delete(k); return true })
			fedInFlight.Range(func(k, _ any) bool { fedInFlight.Delete(k); return true })

			deps := analyze.Deps{LocalRepoDirs: []string{parent}}
			res, err := handleFederatedCoChangeCoreWithBudget(context.Background(), FederatedCoChangeArgs{
				Repos: "acme-*", WindowHours: 24, MinPairs: 2,
			}, deps, tc.budget)
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if res.IsError {
				t.Fatalf("handler must not return MCP error; body=%s", extractText(t, res))
			}
			body := extractText(t, res)
			if strings.Contains(body, `"pairs": null`) {
				t.Fatalf("pairs must never be null; body=%s", body)
			}
			var out FederatedCoChangeResult
			if err := json.Unmarshal([]byte(body), &out); err != nil {
				t.Fatalf("response must be valid JSON: %v\nbody=%s", err, body)
			}
			if out.Pairs == nil {
				t.Fatalf("Pairs slice must be non-nil (empty []VerifiedPair, not nil); body=%s", body)
			}
		})
	}
}
