package langutil

import "testing"

func TestCallerKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		path string
		want string
	}{
		{"DoThing", "pkg/worker.go", "production"},
		{"do_thing", "pkg/worker.py", "production"},
		{"DoThing", "pkg/worker_test.go", "test"},
		{"helper", "pkg/worker_test.go", "test"},
		{"TestFoo", "pkg/foo_test.go", "test"},
		{"Test_Foo_empty", "pkg/foo_test.go", "test"},
		{"ExampleFoo", "pkg/foo_test.go", "example"},
		{"BenchmarkFoo", "pkg/foo_test.go", "benchmark"},
		{"FuzzFoo", "pkg/foo_test.go", "test"},
		{"test_foo", "test_foo.py", "test"},
		{"ExampleFoo", "pkg/example.go", "production"},
		{"BenchmarkFoo", "pkg/bench.go", "production"},
		{"TestHelper", "pkg/foo.go", "production"},
		{"FuzzTarget", "pkg/fuzz.go", "production"},
	}
	for _, tc := range cases {
		t.Run(tc.name+"_"+tc.path, func(t *testing.T) {
			got := CallerKind(tc.name, tc.path)
			if got != tc.want {
				t.Errorf("CallerKind(%q, %q) = %q, want %q", tc.name, tc.path, got, tc.want)
			}
		})
	}
}
