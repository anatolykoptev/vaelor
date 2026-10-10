package main

import (
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func prPayload(association string) []byte {
	return []byte(fmt.Sprintf(
		`{"action":"opened","number":7,"repository":{"full_name":"o/r"},`+
			`"pull_request":{"user":{"login":"someone"},"author_association":%q}}`, association))
}

func TestDispatchPullRequest_AuthorAssociationGate(t *testing.T) {
	cases := []struct {
		association string
		wantReview  bool
	}{
		{"OWNER", true},
		{"MEMBER", true},
		{"COLLABORATOR", true},
		{"CONTRIBUTOR", false},
		{"FIRST_TIME_CONTRIBUTOR", false},
		{"FIRST_TIMER", false},
		{"NONE", false},
		{"MANNEQUIN", false},
		{"", false}, // field absent: fail closed
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.association), func(t *testing.T) {
			var reviewed []string
			deps := dispatchDeps{postReview: func(slug string, pr int) error {
				reviewed = append(reviewed, fmt.Sprintf("%s#%d", slug, pr))
				return nil
			}}
			before := testutil.ToFloat64(webhookIgnoredTotal.WithLabelValues(webhookIgnoreUntrustedAuthor))

			DispatchGitHubEvent("pull_request", prPayload(tc.association), deps)

			if got := len(reviewed) == 1; got != tc.wantReview {
				t.Fatalf("association %q: reviewed=%v, want review=%v", tc.association, reviewed, tc.wantReview)
			}
			delta := testutil.ToFloat64(webhookIgnoredTotal.WithLabelValues(webhookIgnoreUntrustedAuthor)) - before
			wantDelta := 0.0
			if !tc.wantReview {
				wantDelta = 1
			}
			if delta != wantDelta {
				t.Errorf("ignored counter delta = %v, want %v", delta, wantDelta)
			}
		})
	}
}
