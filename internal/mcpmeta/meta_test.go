package mcpmeta

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEnvelope_WrapWithHint(t *testing.T) {
	t.Parallel()
	env := Wrap(50*time.Millisecond, "use understand(symbol=Foo) for the body")
	if env.DurationMS != 50 {
		t.Fatalf("DurationMS: got %d, want 50", env.DurationMS)
	}
	if env.Hint != "use understand(symbol=Foo) for the body" {
		t.Fatalf("Hint: got %q", env.Hint)
	}
}

func TestEnvelope_WrapWithoutHint(t *testing.T) {
	t.Parallel()
	env := Wrap(120*time.Millisecond, "")
	if env.Hint != "" {
		t.Fatalf("empty hint must stay empty, got %q", env.Hint)
	}
	if env.DurationMS != 120 {
		t.Fatalf("DurationMS: got %d, want 120", env.DurationMS)
	}
}

func TestEnvelope_JSONShape(t *testing.T) {
	t.Parallel()
	env := Wrap(7*time.Millisecond, "")
	got, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"duration_ms":7}`
	if string(got) != want {
		t.Fatalf("JSON: got %s, want %s", got, want)
	}
}

func TestWrap_SubMillisecondClampedToOne(t *testing.T) {
	t.Parallel()
	env := Wrap(100*time.Microsecond, "")
	if env.DurationMS != 1 {
		t.Fatalf("sub-ms must clamp to 1, got %d", env.DurationMS)
	}
}

func TestEnvelope_JSONShape_FullyPopulated(t *testing.T) {
	t.Parallel()
	env := Envelope{
		DurationMS:   42,
		Hint:         "h",
		StaleWarning: "w",
		IndexedSHA:   "abc",
		LiveSHA:      "def",
	}
	got, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"duration_ms":42,"hint":"h","stale_warning":"w","indexed_sha":"abc","live_sha":"def"}`
	if string(got) != want {
		t.Fatalf("JSON: got %s, want %s", got, want)
	}
}
