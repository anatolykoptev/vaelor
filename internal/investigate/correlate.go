package investigate

import "strings"

// OperationToFuncName extracts a Go-friendly function name from a Jaeger
// span operation name. Handles three shapes:
//
//   - gRPC: "/pkg.Service/Method" → "Method"
//   - HTTP: "GET /api/v1/users" → "users" (last non-empty path segment)
//   - Plain: "ProcessMessage" or "(*Server).Handle" → "ProcessMessage" / "Handle"
//
// Returns empty string if no meaningful identifier can be extracted.
// The output is the symbol name to feed into compound.FindSymbol — best-effort,
// not guaranteed to match an existing function.
func OperationToFuncName(op string) string {
	op = strings.TrimSpace(op)
	if op == "" {
		return ""
	}

	// gRPC shape: starts with "/", contains "/" between path and method.
	if strings.HasPrefix(op, "/") && strings.Count(op, "/") >= 2 {
		idx := strings.LastIndex(op, "/")
		method := op[idx+1:]
		if method != "" {
			return method
		}
	}

	// HTTP shape: starts with HTTP method.
	for _, verb := range []string{"GET ", "POST ", "PUT ", "DELETE ", "PATCH ", "HEAD ", "OPTIONS "} {
		if strings.HasPrefix(op, verb) {
			path := strings.TrimPrefix(op, verb)
			path = strings.SplitN(path, "?", 2)[0]
			path = strings.TrimRight(path, "/")
			parts := strings.Split(path, "/")
			for i := len(parts) - 1; i >= 0; i-- {
				if parts[i] != "" && !strings.HasPrefix(parts[i], ":") {
					return parts[i]
				}
			}
			return ""
		}
	}

	// Receiver-method shape: "(*Type).Method" → "Method".
	if idx := strings.LastIndex(op, ")."); idx >= 0 {
		method := op[idx+2:]
		if method != "" {
			return method
		}
	}

	// Plain identifier — return as-is.
	return op
}
