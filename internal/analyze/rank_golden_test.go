package analyze

import (
	"reflect"
	"sort"
	"testing"
)

// TestExtractKeywordsForBoost_Golden pins the exact output of extractKeywordsForBoost
// on representative inputs. This is the repo_analyze ranking contract: if the
// extract into internal/lextoken changes the output of this function on any of
// these inputs, the refactor has altered behaviour and must be reverted.
//
// After the lextoken extract, this test should delegate to lextoken.KeywordTokenize
// via the rewired extractKeywordsForBoost — and still pass byte-identically.
func TestExtractKeywordsForBoost_Golden(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  []string // sorted — order depends on Go map iteration, so we sort
	}{
		{
			name:  "plain_query",
			query: "parse config handler",
			want:  []string{"config", "handler", "parse"},
		},
		{
			name:  "stopwords_only",
			query: "what are the functions",
			want:  []string{"functions"}, // "what"/"are"/"the" are stopwords; "functions" passes
		},
		{
			name:  "code_domain_stopwords",
			query: "what code is in file function method",
			// "what"/"code"/"file"/"function"/"method" → stopwords; "is"/"in" → <3 chars
			want: []string{},
		},
		{
			name:  "camel_not_split",
			query: "handleUserAuth middleware",
			// KeywordTokenize does NOT identifier-split; lowercase whole-word only
			want: []string{"handleuserauth", "middleware"},
		},
		{
			name:  "dedup",
			query: "parse parse config",
			want:  []string{"config", "parse"},
		},
		{
			name:  "all_stopwords_plus_domain",
			query: "the and for that with this from are not have function method code file which where when how what",
			want:  []string{},
		},
		{
			name:  "short_terms_filtered",
			query: "go an it",
			// "go" → 2 chars → filtered; "an" → 2 chars → filtered; "it" → 2 chars → filtered
			want: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractKeywordsForBoost(tc.query)
			sortedGot := make([]string, len(got))
			copy(sortedGot, got)
			sort.Strings(sortedGot)
			if len(sortedGot) == 0 && len(tc.want) == 0 {
				return // both empty → pass
			}
			if !reflect.DeepEqual(sortedGot, tc.want) {
				t.Errorf("extractKeywordsForBoost(%q):\n  got  %v\n  want %v",
					tc.query, sortedGot, tc.want)
			}
		})
	}
}
