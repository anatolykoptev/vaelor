package federate

import (
	"context"
	"strings"

	"github.com/anatolykoptev/go-code/internal/gitutil"
	"github.com/anatolykoptev/go-code/internal/slugparse"
)

// dedupeByOrigin collapses repos that share a git origin identity, keeping the
// first occurrence (roots arrive in lexical order from repofind.Discover, so
// "oxpulse-chat" wins over "oxpulse-chat-dev"). Repos with no origin remote
// are kept as-is — each is treated as distinct. Order is preserved.
func dedupeByOrigin(roots []string) []string {
	seen := make(map[string]bool, len(roots))
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		origin := gitutil.OriginURL(context.Background(), root)
		id := repoIdentity(origin)
		if id != "" {
			if seen[id] {
				continue
			}
			seen[id] = true
		}
		out = append(out, root)
	}
	return out
}

// repoIdentity canonicalizes a git origin URL to a stable identity for dedup.
// Reuses internal/slugparse (canonicalizes SSH "git@github.com:owner/repo.git"
// and HTTPS "https://github.com/owner/repo" → "owner/repo"). For unknown hosts
// / unparseable inputs slugparse errors; we fall back to the raw trimmed origin
// so two identical raw remotes still collapse by exact match. Empty → "".
func repoIdentity(origin string) string {
	o := strings.TrimSpace(origin)
	if o == "" {
		return ""
	}
	if slug, err := slugparse.Parse(o); err == nil {
		return slug
	}
	return o
}
