package main

import (
	"testing"
)

func TestRelaxQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "single word returns nil",
			query: "chromiumoxide",
			want:  nil,
		},
		{
			name:  "two words drops last",
			query: "headless browser",
			want:  []string{"headless"},
		},
		{
			name:  "three words returns two strategies",
			query: "chromiumoxide rust browser",
			want:  []string{"chromiumoxide rust", "chromiumoxide"},
		},
		{
			name:  "preserves github syntax",
			query: "headless browser language:rust",
			want:  []string{"headless language:rust"},
		},
		{
			name:  "three words with syntax",
			query: "chromiumoxide rust browser language:rust",
			want:  []string{"chromiumoxide rust language:rust", "chromiumoxide language:rust"},
		},
		{
			name:  "only syntax returns nil",
			query: "language:rust stars:>100",
			want:  nil,
		},
		{
			name:  "single word with syntax returns nil",
			query: "chromiumoxide language:rust",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := relaxQuery(tt.query)
			if len(got) != len(tt.want) {
				t.Fatalf("relaxQuery(%q) = %v, want %v", tt.query, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("relaxQuery(%q)[%d] = %q, want %q", tt.query, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestWebSearchQueries(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantLen int
	}{
		{
			name:    "short query gets 2 variations",
			query:   "chromiumoxide",
			wantLen: 2,
		},
		{
			name:    "long query gets 3 variations",
			query:   "chromiumoxide rust headless browser",
			wantLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := webSearchQueries(tt.query)
			if len(got) != tt.wantLen {
				t.Errorf("webSearchQueries(%q) returned %d queries, want %d: %v",
					tt.query, len(got), tt.wantLen, got)
			}
		})
	}
}
