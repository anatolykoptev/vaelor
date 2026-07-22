// Package argnorm implements vaelor-side pre-validation argument
// normalization for MCP tool calls. It sits in front of the go-mcpserver
// framework's strict JSON-schema validation (which hard-fails on unknown
// properties with "unexpected additional properties") and:
//
//   - strips unknown properties before they reach framework validation,
//     appending a one-line note to the response naming the ignored params
//     and the supported set (tolerant reader, issue #568);
//   - promotes recurring agent-expected param names to their canonical
//     counterparts (aliases: limit→max_results / top_k, insights→repo, …);
//   - rewrites unambiguous tool-name aliases (github_repo_search→
//     github_code_search) and emits did-you-mean suggestions for unknown
//     tools (issue #570).
//
// The framework seam itself (go-mcpserver/lenient.go: resolved.Validate)
// remains strict — this package is the vaelor-side normalization layer that
// runs BEFORE that validation via an MCP receiving middleware. Changing the
// framework to tolerate unknown properties natively is a BLOCKED item tracked
// separately; see the task report.
package argnorm

import (
	"context"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolSpec is the per-tool metadata recorded at registration time.
type toolSpec struct {
	// accepted is the set of top-level JSON property names the tool's input
	// schema declares. Unknown properties not in this set are stripped before
	// framework validation.
	accepted map[string]struct{}
	// open is true for tools with an open schema (accept anything). When true,
	// stripping is skipped entirely. This is distinct from a closed empty
	// struct (struct{}) which has open=false and accepted=empty — such a tool
	// accepts NO params, and any args should be stripped (#581).
	open bool
}

// Registry records the accepted property set for every tool registered through
// AddTool. It is populated during server bootstrap (single-goroutine, write-once)
// and read by the normalization middleware on every tools/call. The mutex guards
// the read path for tests that construct a Registry concurrently with lookup.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]toolSpec
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]toolSpec)}
}

// Count returns the number of registered tools.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// Register records the accepted property names for a tool as a CLOSED schema
// — only the listed properties are accepted, and any unknown properties are
// stripped. An empty accepted slice means the tool accepts NO params (e.g.
// struct{}), which is distinct from an open schema (#581). Exported for tests
// that build a Registry without a full server. Production registration goes
// through AddTool, which reflects the property names from In.
func (r *Registry) Register(name string, accepted []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	set := make(map[string]struct{}, len(accepted))
	for _, p := range accepted {
		if p != "" && p != "-" {
			set[p] = struct{}{}
		}
	}
	r.tools[name] = toolSpec{accepted: set, open: false}
}

// RegisterOpen records a tool with an OPEN schema — any properties are
// accepted, stripping is skipped. Used by AddTool for non-struct input types
// or structs with no json-tagged fields where the accepted set cannot be
// determined by reflection (#581).
func (r *Registry) RegisterOpen(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = toolSpec{accepted: nil, open: true}
}

// Has reports whether name is a registered tool.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	_, ok := r.tools[name]
	r.mu.RUnlock()
	return ok
}

// Accepted returns the accepted property set for name and whether the tool has
// an open schema. ok is false for unknown tools. A closed empty struct (struct{})
// has open=false and an empty accepted set — it accepts NO params, so any args
// are stripped (#581).
func (r *Registry) Accepted(name string) (accepted map[string]struct{}, open, ok bool) {
	r.mu.RLock()
	spec, exists := r.tools[name]
	r.mu.RUnlock()
	if !exists {
		return nil, false, false
	}
	return spec.accepted, spec.open, true
}

// Names returns the registered tool names in registration-independent sorted
// order, used for did-you-mean matching.
func (r *Registry) Names() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	r.mu.RUnlock()
	return names
}

// defaultRegistry is the package-level Registry used by AddTool when no
// explicit Registry is passed. It backs the production middleware wired in
// main.go. Tests use NewRegistry + Register to build isolated registries.
var defaultRegistry = NewRegistry()

// Default returns the package-level Registry populated by AddTool.
func Default() *Registry { return defaultRegistry }

// AddTool registers a tool with lenient input validation (delegating to
// mcpserver.AddTool) AND records the tool's accepted JSON property names in
// the default Registry so the normalization middleware can strip unknowns.
//
// It is a drop-in replacement for mcpserver.AddTool: identical signature and
// semantics. The only addition is the Registry side-effect.
func AddTool[In any](s *mcp.Server, t *mcp.Tool, h func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, error)) {
	props, isStruct := jsonProperties(reflect.TypeFor[In]())
	if !isStruct || props == nil {
		// Open schema: non-struct type, or struct with no json-tagged fields
		// (can't determine accepted set by reflection). Stripping disabled.
		defaultRegistry.RegisterOpen(t.Name)
		slog.Debug("argnorm: open-schema tool (no json-tagged fields), stripping disabled",
			slog.String("tool", t.Name))
	} else {
		// Closed schema: struct with json-tagged fields, or struct{} (zero
		// fields → empty props, accepts NO params). props is non-nil.
		defaultRegistry.Register(t.Name, props)
	}
	mcpserver.AddTool(s, t, h)
}

// jsonProperties returns the top-level JSON property names declared on a struct
// type by reflecting its `json:"name,..." tags. Anonymous (embedded) fields are
// recursed so promoted fields are included. Names that are "-" or empty (the
// json:"-" / untagged sentinel) are excluded — they are not accepted input
// properties.
//
// Returns (props, isStruct):
//   - Non-struct type → (nil, false): open schema.
//   - struct{} (zero fields) → ([]string{}, true): closed schema, accepts NO
//     params. Distinct from open — any args should be stripped (#581).
//   - Struct with fields but no json-tagged fields → (nil, true): open schema
//     (can't determine accepted set by reflection).
//   - Struct with json-tagged fields → (props, true): closed schema.
func jsonProperties(t reflect.Type) (props []string, isStruct bool) {
	if t == nil {
		return nil, false
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, false
	}
	// It's a struct — initialize props to non-nil empty so struct{} (zero
	// fields) is distinguishable from non-struct (nil). collectJSONProps
	// appends to props; if no json-tagged fields are found, props stays
	// non-nil empty for struct{}, nil for structs with untagged fields.
	props = []string{}
	collectJSONProps(t, &props)
	if len(props) == 0 {
		// Struct with fields but no json tags → can't determine accepted set.
		// Return nil to signal open schema. struct{} (zero fields) also lands
		// here but with NumField()==0 — distinguish by checking field count.
		if t.NumField() == 0 {
			// struct{} → closed, accepts no params. Keep non-nil empty.
			return props, true
		}
		// Struct with fields but no json tags → open.
		return nil, true
	}
	return props, true
}

// JsonProperty is the exported form of jsonProperties, for use by callers
// outside internal/argnorm (e.g. handler tests in cmd/vaelor that need to
// assert a tool's input schema is closed/non-empty without spinning up a full
// server). See jsonProperties for the return-value semantics.
func JsonProperty(t reflect.Type) (props []string, isStruct bool) {
	return jsonProperties(t)
}

func collectJSONProps(t reflect.Type, out *[]string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		// Recurse into anonymous (embedded) fields to pick up promoted props.
		if f.Anonymous {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				collectJSONProps(ft, out)
				continue
			}
		}
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		*out = append(*out, name)
	}
}
