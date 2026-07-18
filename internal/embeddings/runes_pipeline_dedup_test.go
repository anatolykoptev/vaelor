package embeddings

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// TestRuneStatementFormDedupKeys verifies that multiple $effect (and $effect.pre)
// expression_statement runes in the same .svelte file produce distinct dedup keys
// when processed by the indexer pipeline.
//
// The dedup key used by indexRepo is: relPath + ":" + sym.Name.
// Before the fix, two $effect statements both emitted Name="$effect", producing
// identical keys → the second symbol would be silently dropped from the embed batch,
// and an eventual DB Upsert would collide on (repo_key, file_path, symbol_name) PK.
// After the fix, each emits "$effect:L<n>", giving distinct keys.
//
// This test bypasses collectSymbols (which filters KindRune out of the embeddings
// pipeline) and instead calls ParseFile directly, then simulates the dedup logic
// from pipeline.go to confirm all rune symbols would produce distinct keys if they
// were ever included in the index.
func TestRuneStatementFormDedupKeys(t *testing.T) {
	const relPath = "src/component.svelte"
	src := []byte(`<script>
  let a = $state(0);
  let b = $state(1);
  $effect(() => { console.log(a); });
  $effect(() => { console.log(b); });
  $effect.pre(() => { console.log('pre-a'); });
  $effect.pre(() => { console.log('pre-b'); });
</script>`)

	result, err := parser.ParseFile(relPath, src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Simulate the dedup loop from pipeline.go:indexRepo.
	// Key = relPath + ":" + sym.Name (identical to what embedAndUpsert would receive).
	seen := make(map[string]int) // key → count
	for _, sym := range result.Symbols {
		if sym.Kind != parser.KindRune {
			continue
		}
		key := relPath + ":" + sym.Name
		seen[key]++
	}

	// No dedup key should appear more than once — a duplicate means a PK collision
	// would occur in the DB when rune symbols are indexed.
	for key, count := range seen {
		if count > 1 {
			t.Errorf("duplicate dedup key %q (count=%d) — would cause DB PK collision", key, count)
		}
	}

	// Confirm the two $state bindings produce distinct token keys (secondary symbols).
	stateTokenKeys := 0
	for key := range seen {
		if strings.Contains(key, "$state:L") {
			stateTokenKeys++
		}
	}
	if stateTokenKeys != 2 {
		t.Errorf("expected 2 $state:L* dedup keys (one per binding), got %d; all rune keys: %v",
			stateTokenKeys, allRuneKeys(seen))
	}

	// Confirm 4 distinct effect dedup keys: 2 $effect:L* + 2 $effect:L* (for $effect.pre,
	// normalised to $effect by runeTokenName). Both map to "$effect:L<n>".
	effectTokenKeys := 0
	for key := range seen {
		if strings.Contains(key, "$effect:L") {
			effectTokenKeys++
		}
	}
	if effectTokenKeys != 4 {
		t.Errorf("expected 4 $effect:L* dedup keys (2 $effect + 2 $effect.pre statements), got %d; all rune keys: %v",
			effectTokenKeys, allRuneKeys(seen))
	}
}

// allRuneKeys returns the rune-related dedup keys for test failure messages.
func allRuneKeys(seen map[string]int) []string {
	var out []string
	for k := range seen {
		out = append(out, k)
	}
	// Insertion sort — small slices only.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
