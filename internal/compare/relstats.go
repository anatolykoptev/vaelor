package compare

import (
	"github.com/anatolykoptev/go-code/internal/parser"
)

// RelStats summarizes type relationship counts for a repository.
type RelStats struct {
	Total          int `json:"total"`
	Extends        int `json:"extends"`
	Implements     int `json:"implements"`
	Embeds         int `json:"embeds"`
	UniqueSubjects int `json:"uniqueSubjects"`
}

// ComputeRelStats computes relationship statistics from extracted relationships.
// Returns nil if no relationships exist.
func ComputeRelStats(rels []parser.TypeRelationship) *RelStats {
	if len(rels) == 0 {
		return nil
	}

	stats := &RelStats{Total: len(rels)}
	subjects := make(map[string]bool)

	for _, r := range rels {
		subjects[r.Subject] = true
		switch r.Kind {
		case parser.RelExtends:
			stats.Extends++
		case parser.RelImplements:
			stats.Implements++
		case parser.RelEmbeds:
			stats.Embeds++
		}
	}

	stats.UniqueSubjects = len(subjects)
	return stats
}
