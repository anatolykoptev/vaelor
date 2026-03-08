package forge

// Registry holds configured forge implementations and dispatches by URL.
type Registry struct {
	forges map[ForgeKind]Forge
}

// NewRegistry creates an empty forge registry.
func NewRegistry() *Registry {
	return &Registry{forges: make(map[ForgeKind]Forge)}
}

// Register adds a forge implementation for the given kind.
func (r *Registry) Register(kind ForgeKind, f Forge) {
	r.forges[kind] = f
}

// Get returns the forge for the given kind, or nil if not registered.
func (r *Registry) Get(kind ForgeKind) Forge {
	return r.forges[kind]
}

// ForURL detects the forge from the input URL/slug and returns the matching
// implementation. Returns nil if no matching forge is registered.
func (r *Registry) ForURL(input string) Forge {
	kind := DetectForge(input)
	if kind == Unknown {
		return nil
	}
	return r.forges[kind]
}
