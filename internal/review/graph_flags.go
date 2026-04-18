package review

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/anatolykoptev/go-code/internal/graphx"
)

// highSurpriseThreshold is the minimum surprise score that triggers a
// "high_surprise" flag on a changed symbol. Empirically, surprise > 0.5
// correlates with cross-community calls that review should flag — they
// indicate the symbol reaches into a module cluster it doesn't normally belong
// to, which raises the chance of unexpected side-effects on merge.
const highSurpriseThreshold = 0.5

// ApplyGraphFlags enriches each changed symbol with Flag/Note based on
// persistent-graph signals (community membership and structural surprise).
// Noop when analytics is nil or repoKey is empty.
//
// beforeCommunity maps "symbolName:file" to the community label recorded
// before the change (e.g. from a pre-merge-base snapshot). When the map is
// nil or empty the community_move branch can never fire, and only
// high_surprise flags are emitted.
//
// A pre-existing non-empty Flag on a symbol is never overwritten.
func ApplyGraphFlags(
	ctx context.Context,
	analytics graphx.Analytics,
	repoKey string,
	symbols []ChangedSymbol,
	beforeCommunity map[string]string,
) {
	if analytics == nil || repoKey == "" {
		return
	}
	for i := range symbols {
		s := &symbols[i]
		if s.Symbol == nil {
			continue
		}
		sig, err := analytics.Symbol(ctx, repoKey, s.Symbol.Name, s.Symbol.File)
		if err != nil {
			slog.Debug("graph_flags: symbol lookup error",
				slog.String("symbol", s.Symbol.Name),
				slog.Any("error", err),
			)
			continue
		}
		if !sig.Found {
			continue
		}

		key := s.Symbol.Name + ":" + s.Symbol.File
		before := beforeCommunity[key]

		if before != "" && before != sig.Community {
			// Community moved: the symbol migrated to a different structural cluster.
			if s.Flag == "" {
				s.Flag = "community_move"
				s.Note = fmt.Sprintf("community changed %s → %s", before, sig.Community)
			}
			continue
		}

		if sig.Surprise >= highSurpriseThreshold && s.Flag == "" {
			s.Flag = "high_surprise"
			s.Note = fmt.Sprintf("surprise score %.2f (threshold %.2f)", sig.Surprise, highSurpriseThreshold)
		}
	}
}
