package prompts

import "strings"

// Intent represents the classified intent of a user query.
type Intent string

const (
	// IntentArchitecture targets high-level design and structure questions.
	IntentArchitecture Intent = "architecture"

	// IntentDebug targets bug-finding, error tracing, and fix-oriented questions.
	IntentDebug Intent = "debug"

	// IntentNavigate targets "where is X defined?" location questions.
	IntentNavigate Intent = "navigate"

	// IntentDependency targets import/dependency graph questions.
	IntentDependency Intent = "dependency"

	// IntentGeneral is the fallback for unclassified queries.
	IntentGeneral Intent = "general"
)

// intentKeywords maps each intent to its trigger keywords.
// Longer phrases (e.g. "nil pointer") are matched first via phrase pass,
// then individual words are scored in a second pass.
var intentKeywords = map[Intent][]string{
	IntentArchitecture: {
		"architecture", "design", "pattern", "structure", "module",
		"organized", "layered", "component", "overview", "approach",
		"designed", "architected",
	},
	IntentDebug: {
		"bug", "error", "fail", "crash", "panic", "nil pointer",
		"race condition", "fix", "broken", "wrong", "issue", "cause",
		"debug", "500", "404", "timeout",
	},
	IntentNavigate: {
		"where", "find", "locate", "defined", "definition", "show me",
		"which file", "what file", "contains", "look for", "search for",
	},
	IntentDependency: {
		"import", "depend", "dependency", "graph",
		"packages depend", "module depend", "coupling",
	},
}

// ClassifyIntent determines the intent of a user query by scoring keyword hits.
// Empty queries return IntentGeneral.
func ClassifyIntent(query string) Intent {
	if strings.TrimSpace(query) == "" {
		return IntentGeneral
	}

	lower := strings.ToLower(query)
	scores := make(map[Intent]int, len(intentKeywords))

	for intent, keywords := range intentKeywords {
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				scores[intent]++
			}
		}
	}

	best := IntentGeneral
	bestScore := 0
	// Deterministic tie-breaking: iterate in fixed order.
	for _, intent := range []Intent{IntentArchitecture, IntentDebug, IntentNavigate, IntentDependency} {
		if s := scores[intent]; s > bestScore {
			bestScore = s
			best = intent
		}
	}

	return best
}

// SystemPromptForIntent returns a system prompt tailored to the classified intent,
// with depth overrides taking priority.
func SystemPromptForIntent(intent Intent, depth string) string {
	// Depth overrides intent — explicit depth always wins.
	switch depth {
	case "overview":
		return SystemPromptOverview
	case "deep":
		return SystemPromptDeep
	}

	switch intent {
	case IntentArchitecture:
		return systemPromptArchitecture
	case IntentDebug:
		return systemPromptDebug
	case IntentNavigate:
		return systemPromptNavigate
	case IntentDependency:
		return SystemPromptDepGraph
	default:
		return SystemPromptRepoAnalysis
	}
}

const systemPromptArchitecture = `You are a senior software architect analyzing a code repository.
Focus on: design patterns, architectural decisions, module boundaries, separation of concerns.
Explain the high-level structure first, then drill into key components.
Reference specific packages and their responsibilities.
Highlight strengths and potential improvements in the architecture.`

const systemPromptDebug = `You are a senior software engineer debugging a code issue.
Focus on: error handling paths, edge cases, race conditions, null/nil checks.
Trace the execution flow from the suspected entry point.
Identify potential root causes and suggest fixes with specific code references.
Prioritize the most likely cause first.`

const systemPromptNavigate = `You are a code navigator helping locate specific code elements.
Focus on: exact file paths, line numbers, function signatures.
Provide the precise location of the requested symbol or pattern.
Show how it connects to related code (callers, callees, types used).
Be direct — answer with locations first, context second.`
