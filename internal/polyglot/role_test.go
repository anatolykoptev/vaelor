package polyglot

import "testing"

func TestClassifyRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sources []string
		want    string
	}{
		{
			name:    "go http server",
			sources: []string{`http.ListenAndServe(":8080", nil)`},
			want:    "server",
		},
		{
			name:    "express server",
			sources: []string{`const app = express()`},
			want:    "server",
		},
		{
			name:    "flask server",
			sources: []string{`app = Flask(__name__)`},
			want:    "server",
		},
		{
			name:    "spring boot",
			sources: []string{`SpringApplication.run(App.class, args)`},
			want:    "server",
		},
		{
			name:    "react client",
			sources: []string{`ReactDOM.render(<App />, root)`},
			want:    "client",
		},
		{
			name:    "fetch client",
			sources: []string{`fetch('/api/users')`},
			want:    "client",
		},
		{
			name:    "ml worker",
			sources: []string{"import torch", "model = torch.nn.Linear(10, 5)"},
			want:    "worker",
		},
		{
			name:    "no signals",
			sources: []string{`func add(a, b int) int { return a + b }`},
			want:    "library",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := classifyRole(tt.sources)
			if got != tt.want {
				t.Errorf("classifyRole() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyLayerRole(t *testing.T) {
	t.Parallel()

	// Public wrapper should produce the same result as the private function.
	sources := []string{`http.ListenAndServe(":8080", nil)`}

	got := ClassifyLayerRole(sources)
	if got != "server" {
		t.Errorf("ClassifyLayerRole() = %q, want %q", got, "server")
	}
}
