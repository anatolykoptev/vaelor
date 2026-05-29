package mcpmeta

import (
	"testing"
	"time"
)

func TestEnvelope_WrapWithHint(t *testing.T) {
	env := Wrap(50*time.Millisecond, "use understand(symbol=Foo) for the body")
	if env.TimingMS != 50 {
		t.Fatalf("TimingMS: got %d, want 50", env.TimingMS)
	}
	if env.Hint != "use understand(symbol=Foo) for the body" {
		t.Fatalf("Hint: got %q", env.Hint)
	}
}

func TestEnvelope_WrapWithoutHint(t *testing.T) {
	env := Wrap(120*time.Millisecond, "")
	if env.Hint != "" {
		t.Fatalf("empty hint must stay empty, got %q", env.Hint)
	}
	if env.TimingMS != 120 {
		t.Fatalf("TimingMS: got %d, want 120", env.TimingMS)
	}
}

func TestEnvelope_JSONShape(t *testing.T) {
	env := Wrap(7*time.Millisecond, "")
	got, err := env.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"timing_ms":7}`
	if string(got) != want {
		t.Fatalf("JSON: got %s, want %s", got, want)
	}
}
