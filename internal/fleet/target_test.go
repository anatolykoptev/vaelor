package fleet_test

import (
	"errors"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fleet"
)

func TestParseTarget(t *testing.T) {
	t.Parallel()

	type tc struct {
		input   string
		want    fleet.Target
		wantErr bool
	}

	tests := []tc{
		{
			input: "",
			want:  fleet.Target{Scheme: "local", Raw: ""},
		},
		{
			input: "local://",
			want:  fleet.Target{Scheme: "local", Raw: "local://"},
		},
		{
			input: "LOCAL://",
			want:  fleet.Target{Scheme: "local", Raw: "LOCAL://"},
		},
		{
			input: "docker://",
			want:  fleet.Target{Scheme: "docker", Raw: "docker://"},
		},
		{
			input: "ssh://krolik",
			want:  fleet.Target{Scheme: "ssh", Host: "krolik", Raw: "ssh://krolik"},
		},
		{
			input: "ssh://ubuntu@hully",
			want:  fleet.Target{Scheme: "ssh", User: "ubuntu", Host: "hully", Raw: "ssh://ubuntu@hully"},
		},
		{
			input: "ssh://krolik:2222",
			want:  fleet.Target{Scheme: "ssh", Host: "krolik", Port: 2222, Raw: "ssh://krolik:2222"},
		},
		{
			input: "ssh://ubuntu@hully:2222",
			want:  fleet.Target{Scheme: "ssh", User: "ubuntu", Host: "hully", Port: 2222, Raw: "ssh://ubuntu@hully:2222"},
		},
		// error cases
		{input: "ssh://", wantErr: true},
		{input: "ssh://krolik:0", wantErr: true},
		{input: "ssh://krolik:99999", wantErr: true},
		{input: "ssh://krolik:abc", wantErr: true},
		{input: "http://krolik", wantErr: true},
		{input: "local://something", wantErr: true},
		{input: "docker://krolik", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := fleet.ParseTarget(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseTarget(%q) = %+v, nil; want error", tt.input, got)
				}
				if !errors.Is(err, fleet.ErrInvalidTarget) {
					t.Fatalf("ParseTarget(%q) error = %v; want errors.Is(ErrInvalidTarget)", tt.input, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTarget(%q) unexpected error: %v", tt.input, err)
			}
			if got.Raw != tt.input {
				t.Errorf("Target.Raw = %q; want %q", got.Raw, tt.input)
			}
			if got.Scheme != tt.want.Scheme {
				t.Errorf("Target.Scheme = %q; want %q", got.Scheme, tt.want.Scheme)
			}
			if got.Host != tt.want.Host {
				t.Errorf("Target.Host = %q; want %q", got.Host, tt.want.Host)
			}
			if got.User != tt.want.User {
				t.Errorf("Target.User = %q; want %q", got.User, tt.want.User)
			}
			if got.Port != tt.want.Port {
				t.Errorf("Target.Port = %d; want %d", got.Port, tt.want.Port)
			}
		})
	}
}
