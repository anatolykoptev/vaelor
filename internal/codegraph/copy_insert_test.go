package codegraph

import (
	"encoding/json"
	"testing"
)

// TestAgtypeJSON_RoundTrip locks in that props survive JSON serialisation
// byte-identically — mutations that emit invalid JSON fail this.
func TestAgtypeJSON_RoundTrip(t *testing.T) {
	t.Parallel()
	props := map[string]string{
		"sig":  `func f(x int) string { return "q\n" }`,
		"html": `<a href="x">&amp;</a>`,
		"back": `c:\path\to`,
	}
	out, err := agtypeJSON(props)
	if err != nil {
		t.Fatalf("agtypeJSON: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	for k, v := range props {
		if got[k] != v {
			t.Errorf("key %q: got %q, want %q", k, got[k], v)
		}
	}

	empty, err := agtypeJSON(nil)
	if err != nil || empty != "{}" {
		t.Errorf("nil props = %q, err %v; want {}", empty, err)
	}
}
