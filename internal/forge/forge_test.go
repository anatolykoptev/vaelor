package forge

import "testing"

func TestForgeKindString(t *testing.T) {
	tests := []struct {
		kind ForgeKind
		want string
	}{
		{GitHub, "github"},
		{GitLab, "gitlab"},
		{Unknown, "unknown"},
		{ForgeKind(99), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("ForgeKind(%d).String() = %q, want %q", tc.kind, got, tc.want)
		}
	}
}
