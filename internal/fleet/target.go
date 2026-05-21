package fleet

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ErrInvalidTarget is returned by ParseTarget when the input cannot be parsed
// into a valid Target.
var ErrInvalidTarget = errors.New("fleet: invalid target")

// ParseTarget normalises a target URL into a Target.
//
// Accepted forms:
//
//	""                            → {Scheme:"local"}
//	"local://"                    → {Scheme:"local"}
//	"docker://"                   → {Scheme:"docker"}
//	"ssh://<host>"                → {Scheme:"ssh", Host:"<host>"}
//	"ssh://<user>@<host>"         → {Scheme:"ssh", User:"<user>", Host:"<host>"}
//	"ssh://<host>:<port>"         → {Scheme:"ssh", Host:"<host>", Port:<port>}
//	"ssh://<user>@<host>:<port>"
//
// Anything else returns an error wrapping ErrInvalidTarget.
//
// Rules:
//   - Host MUST be non-empty for ssh.
//   - Host MUST be empty for local/docker (MVP: docker is local-socket only).
//   - Port MUST be in 1..65535 if specified.
//   - Scheme is case-folded to lowercase in the result.
//
// Target.Raw is always set to the original input.
func ParseTarget(raw string) (Target, error) {
	// Empty string → local.
	if raw == "" {
		return Target{Scheme: "local", Raw: raw}, nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return Target{}, fmt.Errorf("%w: %s", ErrInvalidTarget, err)
	}

	scheme := strings.ToLower(u.Scheme)

	switch scheme {
	case "local", "docker":
		// Both local:// and docker:// are local-only in MVP.
		// Host and path must be empty (only the authority separator "//" is allowed).
		if u.Host != "" {
			return Target{}, fmt.Errorf("%w: %q scheme must have empty host, got %q", ErrInvalidTarget, scheme, u.Host)
		}
		if u.Path != "" && u.Path != "/" {
			return Target{}, fmt.Errorf("%w: %q scheme must have empty path, got %q", ErrInvalidTarget, scheme, u.Path)
		}
		if u.RawQuery != "" {
			return Target{}, fmt.Errorf("%w: %q scheme must have no query", ErrInvalidTarget, scheme)
		}
		if u.Fragment != "" {
			return Target{}, fmt.Errorf("%w: %q scheme must have no fragment", ErrInvalidTarget, scheme)
		}
		return Target{Scheme: scheme, Raw: raw}, nil

	case "ssh":
		host := u.Hostname()
		if host == "" {
			return Target{}, fmt.Errorf("%w: ssh target requires a non-empty host", ErrInvalidTarget)
		}

		var user string
		if u.User != nil {
			user = u.User.Username()
		}

		var port int
		if portStr := u.Port(); portStr != "" {
			p, err := strconv.Atoi(portStr)
			if err != nil {
				return Target{}, fmt.Errorf("%w: ssh port %q is not a valid integer", ErrInvalidTarget, portStr)
			}
			if p < 1 || p > 65535 {
				return Target{}, fmt.Errorf("%w: ssh port %d out of range [1,65535]", ErrInvalidTarget, p)
			}
			port = p
		}

		return Target{
			Raw:    raw,
			Scheme: "ssh",
			Host:   host,
			User:   user,
			Port:   port,
		}, nil

	default:
		return Target{}, fmt.Errorf("%w: unsupported scheme %q", ErrInvalidTarget, scheme)
	}
}
