package memlimit

import (
	"errors"
	"testing"
)

func fakeEnv(vars map[string]string, files map[string]string) env {
	return env{
		getenv: func(k string) string { return vars[k] },
		readFile: func(p string) ([]byte, error) {
			if v, ok := files[p]; ok {
				return []byte(v), nil
			}
			return nil, errors.New("absent")
		},
	}
}

func TestDetect(t *testing.T) {
	const gib = int64(1) << 30
	cases := []struct {
		name  string
		vars  map[string]string
		files map[string]string
		want  int64
		src   string
	}{
		{"v2 limit", nil, map[string]string{cgroupV2Max: "3221225472\n"}, 3 * gib, cgroupV2Max},
		{"v2 unlimited", nil, map[string]string{cgroupV2Max: "max\n"}, 0, ""},
		{"v1 limit", nil, map[string]string{cgroupV1Max: "2147483648"}, 2 * gib, cgroupV1Max},
		{"v1 unlimited sentinel", nil, map[string]string{cgroupV1Max: "9223372036854771712"}, 0, ""},
		{"env wins over cgroup", map[string]string{EnvLimit: "1GiB"}, map[string]string{cgroupV2Max: "3221225472"}, gib, EnvLimit},
		{"env bytes", map[string]string{EnvLimit: "1048576"}, nil, 1 << 20, EnvLimit},
		{"bad env falls through to cgroup", map[string]string{EnvLimit: "lots"}, map[string]string{cgroupV2Max: "1073741824"}, gib, cgroupV2Max},
		{"nothing detectable", nil, nil, 0, ""},
	}
	for _, c := range cases {
		got, src := fakeEnv(c.vars, c.files).detect()
		if got != c.want || src != c.src {
			t.Errorf("%s: got (%d,%q), want (%d,%q)", c.name, got, src, c.want, c.src)
		}
	}
}

func TestParseSize(t *testing.T) {
	cases := map[string]int64{
		"1048576": 1 << 20, "1KiB": 1 << 10, "2MiB": 2 << 20, "3GiB": 3 << 30,
		"4K": 4 << 10, "5M": 5 << 20, "6G": 6 << 30, " 7 MiB ": 7 << 20,
	}
	for in, want := range cases {
		if got, err := ParseSize(in); err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "lots", "1GB", "1MB", "1KB", "1.5GiB", "-"} {
		if got, err := ParseSize(bad); err == nil {
			t.Errorf("ParseSize(%q) = %d, want an error (decimal and fractional sizes are rejected)", bad, got)
		}
	}
}
