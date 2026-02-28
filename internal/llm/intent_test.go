package llm

import "testing"

func TestClassifyIntent_Architecture(t *testing.T) {
	tests := []struct {
		query string
	}{
		{"How is the authentication system designed?"},
		{"Explain the overall architecture"},
		{"What patterns does this codebase use?"},
		{"Describe the module structure"},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			got := ClassifyIntent(tc.query)
			if got != IntentArchitecture {
				t.Errorf("ClassifyIntent(%q) = %q, want %q", tc.query, got, IntentArchitecture)
			}
		})
	}
}

func TestClassifyIntent_Debug(t *testing.T) {
	tests := []struct {
		query string
	}{
		{"Why does the login handler return 500?"},
		{"Find the bug in user validation"},
		{"What causes the nil pointer error?"},
		{"Fix the race condition in cache"},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			got := ClassifyIntent(tc.query)
			if got != IntentDebug {
				t.Errorf("ClassifyIntent(%q) = %q, want %q", tc.query, got, IntentDebug)
			}
		})
	}
}

func TestClassifyIntent_Navigate(t *testing.T) {
	tests := []struct {
		query string
	}{
		{"Where is HandleLogin defined?"},
		{"Find the Config struct"},
		{"Show me the middleware chain"},
		{"What file contains the router setup?"},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			got := ClassifyIntent(tc.query)
			if got != IntentNavigate {
				t.Errorf("ClassifyIntent(%q) = %q, want %q", tc.query, got, IntentNavigate)
			}
		})
	}
}

func TestClassifyIntent_Dependency(t *testing.T) {
	tests := []struct {
		query string
	}{
		{"What imports does the auth package have?"},
		{"Show dependency graph for internal/"},
		{"Which packages depend on util?"},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			got := ClassifyIntent(tc.query)
			if got != IntentDependency {
				t.Errorf("ClassifyIntent(%q) = %q, want %q", tc.query, got, IntentDependency)
			}
		})
	}
}

func TestClassifyIntent_Default(t *testing.T) {
	tests := []struct {
		query string
	}{
		{"Tell me about this repo"},
		{"What does this code do?"},
		{""},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			got := ClassifyIntent(tc.query)
			if got != IntentGeneral {
				t.Errorf("ClassifyIntent(%q) = %q, want %q", tc.query, got, IntentGeneral)
			}
		})
	}
}

func TestSystemPromptForIntent_DepthOverrides(t *testing.T) {
	// Depth "overview" overrides any intent.
	if got := SystemPromptForIntent(IntentDebug, "overview"); got != SystemPromptOverview {
		t.Errorf("depth=overview should return SystemPromptOverview, got different prompt")
	}
	// Depth "deep" overrides any intent.
	if got := SystemPromptForIntent(IntentNavigate, "deep"); got != SystemPromptDeep {
		t.Errorf("depth=deep should return SystemPromptDeep, got different prompt")
	}
}

func TestSystemPromptForIntent_IntentSelection(t *testing.T) {
	tests := []struct {
		intent Intent
		want   string
	}{
		{IntentArchitecture, systemPromptArchitecture},
		{IntentDebug, systemPromptDebug},
		{IntentNavigate, systemPromptNavigate},
		{IntentDependency, SystemPromptDepGraph},
		{IntentGeneral, SystemPromptRepoAnalysis},
	}
	for _, tc := range tests {
		t.Run(string(tc.intent), func(t *testing.T) {
			got := SystemPromptForIntent(tc.intent, "")
			if got != tc.want {
				t.Errorf("SystemPromptForIntent(%q, \"\") returned wrong prompt", tc.intent)
			}
		})
	}
}
