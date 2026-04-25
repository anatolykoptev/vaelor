package deadcode

import (
	"context"
	"log"
	"sync"

	"github.com/anatolykoptev/go-code/internal/oxcodes"
)

const (
	maxOxCodesSymbols  = 50 // don't query more than this many symbols per analysis
	oxCodesScope       = "function_bodies"
	oxCodesMaxResults  = 1  // we only need to know if at least one reference exists
	oxCodesParallelism = 10 // max concurrent ox-codes requests
)

// filterByStringRefs removes from dead any symbol whose name appears as a
// string literal inside function bodies according to ox-codes scoped search.
// This catches callbacks registered by name, reflection-based dispatch, and
// config-driven function references that tree-sitter call graph cannot see.
//
// Errors from ox-codes are logged as warnings and do not fail the analysis —
// the full dead list is returned unchanged on error.
func filterByStringRefs(
	ctx context.Context,
	client *oxcodes.Client,
	root string,
	language string,
	dead []DeadSymbol,
) []DeadSymbol {
	limit := len(dead)
	if limit > maxOxCodesSymbols {
		limit = maxOxCodesSymbols
	}

	// Build a set of names that have string references.
	referenced := make(map[string]bool, limit)
	sem := make(chan struct{}, oxCodesParallelism)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := range limit {
		sym := dead[i]
		wg.Add(1)
		sem <- struct{}{}
		go func(sym DeadSymbol) {
			defer wg.Done()
			defer func() { <-sem }()
			resp, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
				Root:          root,
				Pattern:       sym.Name,
				Scope:         oxCodesScope,
				Language:      language,
				IsRegex:       false,
				MaxResults:    oxCodesMaxResults,
				CaseSensitive: true,
			})
			if err != nil {
				log.Printf("deadcode: ox-codes search for %q: %v (skipping)", sym.Name, err)
				return
			}
			if resp.TotalMatches > 0 {
				mu.Lock()
				referenced[sym.Name] = true
				mu.Unlock()
			}
		}(sym)
	}
	wg.Wait()

	if len(referenced) == 0 {
		return dead
	}

	filtered := dead[:0]
	for _, sym := range dead {
		if !referenced[sym.Name] {
			filtered = append(filtered, sym)
		}
	}
	return filtered
}
