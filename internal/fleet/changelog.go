// internal/fleet/changelog.go
package fleet

// Changelog holds the result of correlating two upstream image tags against
// a public GitHub repository via the Compare API.
//
// Populated by internal/fleet/upstream.Enrich for DiffTagDrift rows.
// Attached to ImageDiff.Changelog (nil for non-TagDrift rows).
type Changelog struct {
	Repo      string            `json:"repo"`                // "minio/minio"
	Base      string            `json:"base"`                // "26.4.25" (actually-resolved tag)
	Head      string            `json:"head"`                // "26.5.3"
	Status    string            `json:"status"`              // "ahead" / "behind" / "diverged" / "identical" / "unresolved"
	Commits   []ChangelogCommit `json:"commits,omitempty"`   // capped at 20, head→base order
	Truncated bool              `json:"truncated,omitempty"` // true if upstream had >20 commits
	Resolved  bool              `json:"resolved"`            // false if mapping or compare failed
	Reason    string            `json:"reason,omitempty"`    // when Resolved=false
	URL       string            `json:"url,omitempty"`       // GitHub web URL for the compare view
}

// ChangelogCommit is one commit entry in a Changelog.
type ChangelogCommit struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	Date    string `json:"date"`    // ISO-8601, from author date
	Subject string `json:"subject"` // first line of commit message
}
