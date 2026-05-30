package coupling

import (
	"reflect"
	"sort"
	"testing"
)

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestExtractSignificantSymbols(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "screaming_snake env vars and consts",
			src:  `let s = std::env::var("RELAY_JWT_SECRET"); const MAX_PEERS = 8;`,
			want: []string{"MAX_PEERS", "RELAY_JWT_SECRET"},
		},
		{
			name: "structured string literals (snake + kebab)",
			src:  `ws.send("peer_joined"); emit('ice-candidate'); type t = "offer";`,
			want: []string{"ice-candidate", "peer_joined"}, // "offer" excluded: no separator
		},
		{
			name: "single screaming word without underscore excluded",
			src:  `let m = "GET"; let p = "POST"; const ERROR = 1;`,
			want: []string{}, // GET/POST/ERROR have no internal separator
		},
		{
			name: "seed stop-set removes generic multi-segment tokens",
			src:  `headers.set("content-type", "x"); let CONTENT_TYPE = 1;`,
			want: []string{}, // content-type / CONTENT_TYPE are stop-listed
		},
		{
			name: "too short or too long rejected",
			// a_b: below minTokenLen (literal path, also filtered by literalRe {5,48})
			// makeLong() string: above maxTokenLen (literal path, filtered by literalRe {5,48})
			// makeScreamingLong(): above maxTokenLen SCREAMING token — only addToken length
			// check blocks this (screamingRe has no length bound), so this subtest
			// specifically validates the addToken guard.
			src:  `let a = "a_b"; let b = "` + makeLong() + `"; const ` + makeScreamingLong() + ` = 1;`,
			want: []string{}, // a_b below minTokenLen; long string above maxTokenLen; screaming above maxTokenLen
		},
		{
			name: "mixed real-world rust",
			src: `match msg { "peer_left" => handle(), _ => {} }
let secret = env::var("RELAY_JWT_SECRET").unwrap();`,
			want: []string{"RELAY_JWT_SECRET", "peer_left"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := keys(extractSignificantSymbols([]byte(tt.src)))
			if tt.want == nil {
				tt.want = []string{}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractSignificantSymbols() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractSignificantSymbols_GenericInfraFloor(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "ubiquitous infra env-vars excluded",
			src:  `DATABASE_URL REDIS_URL MAX_RETRIES LOG_LEVEL LISTEN_ADDR CACHE_HOST SVC_PORT`,
			want: []string{}, // all end in a generic-infra suffix
		},
		{
			name: "domain secret survives (primary signal)",
			src:  `let s = std::env::var("RELAY_JWT_SECRET"); const API_KEY = 1; let t = SESSION_TOKEN;`,
			want: []string{"API_KEY", "RELAY_JWT_SECRET", "SESSION_TOKEN"}, // _SECRET/_KEY/_TOKEN are signal, not infra
		},
		{
			name: "protocol literal survives",
			src:  `ws.send("peer_joined"); emit("ice-candidate");`,
			want: []string{"ice-candidate", "peer_joined"},
		},
		{
			name: "infra-suffixed literal also excluded",
			src:  `let x = "service_url"; let y = "redis_host";`,
			want: []string{}, // literal values with infra suffixes are noise too
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := keys(extractSignificantSymbols([]byte(tt.src)))
			if tt.want == nil {
				tt.want = []string{}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractSignificantSymbols() = %v, want %v", got, tt.want)
			}
		})
	}
}

// makeLong builds a quoted string content longer than maxTokenLen for the bounds test.
func makeLong() string {
	b := make([]byte, maxTokenLen+5)
	for i := range b {
		b[i] = 'a'
	}
	b[3] = '_' // separator, so only length is the rejection reason
	return string(b)
}

// makeScreamingLong builds a SCREAMING_SNAKE_CASE token longer than maxTokenLen.
// screamingRe has no length bound — only addToken's length check blocks it.
func makeScreamingLong() string {
	b := make([]byte, maxTokenLen+5)
	for i := range b {
		b[i] = 'A'
	}
	b[3] = '_' // at least one underscore to satisfy screamingRe
	return string(b)
}
