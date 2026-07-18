package research

import "github.com/anatolykoptev/vaelor/internal/parser"

func makeSymbols(names ...string) []*parser.Symbol {
	out := make([]*parser.Symbol, len(names))
	for i, n := range names {
		out[i] = &parser.Symbol{Name: n, Kind: parser.KindFunction}
	}
	return out
}

func symbolNames(syms []*parser.Symbol) []string {
	names := make([]string, len(syms))
	for i, s := range syms {
		names[i] = s.Name
	}
	return names
}
