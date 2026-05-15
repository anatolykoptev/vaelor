package codegraph

import (
	"testing"
	"time"
)

func TestParseTimeoutSecs(t *testing.T) {
	const key = "GOCODE_TEST_TIMEOUT_S"
	const def = 30 * time.Second

	tests := []struct {
		name    string
		envVal  string // empty string means unset
		want    time.Duration
	}{
		{
			name:   "unset env returns default",
			envVal: "",
			want:   def,
		},
		{
			name:   "valid positive integer",
			envVal: "60",
			want:   60 * time.Second,
		},
		{
			name:   "valid with leading/trailing space",
			envVal: "  45  ",
			want:   45 * time.Second,
		},
		{
			name:   "zero falls back to default",
			envVal: "0",
			want:   def,
		},
		{
			name:   "negative falls back to default",
			envVal: "-5",
			want:   def,
		},
		{
			name:   "garbage string falls back to default",
			envVal: "not-a-number",
			want:   def,
		},
		{
			name:   "float string falls back to default",
			envVal: "3.14",
			want:   def,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envVal == "" {
				t.Setenv(key, "")
			} else {
				t.Setenv(key, tc.envVal)
			}
			got := parseTimeoutSecs(key, def)
			if got != tc.want {
				t.Errorf("parseTimeoutSecs(%q, %v) = %v; want %v", key, def, got, tc.want)
			}
		})
	}
}
