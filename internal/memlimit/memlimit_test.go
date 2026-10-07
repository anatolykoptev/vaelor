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

func TestApply(t *testing.T) {
	var set []int64
	rec := func(n int64) int64 { set = append(set, n); return 0 }

	if got := fakeEnv(nil, map[string]string{cgroupV2Max: "1000000000"}).apply(rec); got != 850000000 || len(set) != 1 || set[0] != 850000000 {
		t.Errorf("limit 1e9: applied %d, set calls %v; want 85%% = 850000000 exactly once", got, set)
	}

	set = nil
	if got := fakeEnv(map[string]string{"GOMEMLIMIT": "1GiB"}, map[string]string{cgroupV2Max: "1000000000"}).apply(rec); got != 0 || len(set) != 0 {
		t.Errorf("explicit GOMEMLIMIT must be honoured (no override): applied %d, calls %v", got, set)
	}

	set = nil
	if got := fakeEnv(nil, nil).apply(rec); got != 0 || len(set) != 0 {
		t.Errorf("no detectable limit must set nothing: applied %d, calls %v", got, set)
	}
}
