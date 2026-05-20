package fleet_test

import (
	"context"
	"errors"
	"testing"

	"github.com/anatolykoptev/go-code/internal/fleet"
)

// fakeProbe is a minimal Probe implementation for testing Registry.
type fakeProbe struct {
	scheme string
}

func (f *fakeProbe) Scheme() string { return f.scheme }
func (f *fakeProbe) List(_ context.Context, _ fleet.Target, _ fleet.Filter) ([]fleet.RuntimeImage, error) {
	return nil, nil
}

func TestRegistry_HasAndGet(t *testing.T) {
	t.Parallel()

	r := fleet.NewRegistry()

	// Not registered initially.
	if r.Has("ssh") {
		t.Error("Has(ssh) = true before Register; want false")
	}

	p := &fakeProbe{scheme: "ssh"}
	r.Register(p)

	if !r.Has("ssh") {
		t.Error("Has(ssh) = false after Register; want true")
	}

	got, err := r.Get(fleet.Target{Scheme: "ssh"})
	if err != nil {
		t.Fatalf("Get(ssh) unexpected error: %v", err)
	}
	if got.Scheme() != "ssh" {
		t.Errorf("Get(ssh).Scheme() = %q; want %q", got.Scheme(), "ssh")
	}
}

func TestRegistry_GetUnknownScheme(t *testing.T) {
	t.Parallel()

	r := fleet.NewRegistry()
	_, err := r.Get(fleet.Target{Scheme: "docker"})
	if err == nil {
		t.Fatal("Get(docker) = nil error; want ErrSchemeUnknown")
	}
	if !errors.Is(err, fleet.ErrSchemeUnknown) {
		t.Errorf("Get(docker) error = %v; want errors.Is(ErrSchemeUnknown)", err)
	}
}

func TestRegistry_LastWinsOverwrite(t *testing.T) {
	t.Parallel()

	r := fleet.NewRegistry()

	first := &fakeProbe{scheme: "local"}
	second := &fakeProbe{scheme: "local"}

	r.Register(first)
	r.Register(second)

	if !r.Has("local") {
		t.Error("Has(local) = false after two Registers; want true")
	}

	got, err := r.Get(fleet.Target{Scheme: "local"})
	if err != nil {
		t.Fatalf("Get(local) unexpected error: %v", err)
	}
	// The second registration must be returned.
	if got != second {
		t.Errorf("Get(local) returned first probe; want second (last-wins)")
	}
}
