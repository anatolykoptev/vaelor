package forge

import "context"

// InlineComment is a single file:line suggestion.
type InlineComment struct {
	Path string // repo-relative
	Line int    // 1-based, on the diff's new side
	Body string
}

// ReviewPayload is the body of a PR review.
type ReviewPayload struct {
	Body     string          // summary markdown
	Event    string          // "COMMENT", "REQUEST_CHANGES", "APPROVE"
	Comments []InlineComment // inline findings
}

// Poster is the write-side of a forge. Separate from Forge so forges that
// cannot post (yet) stay compilable.
type Poster interface {
	PostReview(ctx context.Context, slug string, pr int, p ReviewPayload) (htmlURL string, err error)
	PostIssueComment(ctx context.Context, slug string, number int, body string) (htmlURL string, err error)
}
