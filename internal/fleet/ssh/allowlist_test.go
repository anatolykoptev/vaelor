package ssh_test

import (
	"errors"
	"testing"

	flssh "github.com/anatolykoptev/go-code/internal/fleet/ssh"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "valid basic",
			args:    []string{"krolik", "docker", "ps", "--no-trunc", "--format={{json .}}"},
			wantErr: false,
		},
		{
			name:    "valid user@host",
			args:    []string{"ubuntu@hully", "docker", "ps", "--no-trunc", "--format={{json .}}"},
			wantErr: false,
		},
		{
			name:    "valid with -p flag",
			args:    []string{"-p", "2222", "krolik", "docker", "ps", "--no-trunc", "--format={{json .}}"},
			wantErr: false,
		},
		{
			name:    "docker inspect not allowed",
			args:    []string{"krolik", "docker", "inspect", "abc"},
			wantErr: true,
		},
		{
			name:    "rm -rf not allowed",
			args:    []string{"krolik", "rm", "-rf", "/"},
			wantErr: true,
		},
		{
			name:    "semicolon in host",
			args:    []string{"krolik;rm", "docker", "ps", "--no-trunc", "--format={{json .}}"},
			wantErr: true,
		},
		{
			name:    "semicolon in format flag",
			args:    []string{"krolik", "docker", "ps", "--no-trunc", "--format={{json .}};rm"},
			wantErr: true,
		},
		{
			name:    "command substitution in format",
			args:    []string{"krolik", "docker", "ps", "--format=$(whoami)"},
			wantErr: true,
		},
		{
			name:    "empty args",
			args:    []string{},
			wantErr: true,
		},
		{
			name:    "only host - too short",
			args:    []string{"krolik"},
			wantErr: true,
		},
		// Leading-dash host must be rejected — would be interpreted as ssh flag.
		// url.Parse("ssh://-v") returns Hostname()=="-v"; isValidHost must
		// reject any host whose first byte is -.
		{
			name:    "leading-dash host rejected",
			args:    []string{"-G", "docker", "ps", "--no-trunc", "--format={{json .}}"},
			wantErr: true,
		},
		{
			name:    "leading-dash with user@ still rejected",
			args:    []string{"-v@realhost", "docker", "ps", "--no-trunc", "--format={{json .}}"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := flssh.Validate(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Validate(%v): want error, got nil", tc.args)
				}
				if !errors.Is(err, flssh.ErrAllowlistViolation) {
					t.Errorf("Validate(%v): want errors.Is(err, ErrAllowlistViolation), got: %v", tc.args, err)
				}
			} else {
				if err != nil {
					t.Fatalf("Validate(%v): want nil, got %v", tc.args, err)
				}
			}
		})
	}
}
