package codegraph

func init() {
	templates["communities"] = &Template{
		ID:          "communities",
		Description: "Show community clusters: group symbols by Louvain community with member counts",
		Params:      []string{"limit"},
		Cypher:      "MATCH (s:Symbol) WHERE s.community IS NOT NULL RETURN s.community, count(s) AS size, collect(s.name) AS members ORDER BY size DESC LIMIT {limit}",
		Cols:        3,
	}
	templates["community_members"] = &Template{
		ID:          "community_members",
		Description: "List all symbols in a specific community by its ID",
		Params:      []string{"name"},
		Cypher:      "MATCH (s:Symbol {community: '{name}'}) RETURN s.name, s.kind, s.file ORDER BY s.name",
		Cols:        3,
	}
	templates["surprises"] = &Template{
		ID:          "surprises",
		Description: "Find surprising cross-package dependencies — hidden couplings between packages",
		Params:      []string{"limit"},
		Cypher:      "MATCH (a:Symbol)-[r:CALLS]->(b:Symbol) WHERE a.file <> b.file RETURN a.name, a.file, a.community, b.name, b.file, b.community, a.pagerank, b.pagerank LIMIT 500",
		Cols:        8,
	}
	templates["graph_diff"] = &Template{
		ID:          "graph_diff",
		Description: "Compare current graph with the previous snapshot — show new/removed symbols, edges, community migrations",
		Params:      []string{},
		Cypher:      "MATCH (s:Symbol) RETURN s.name LIMIT 1",
		Cols:        1,
	}
}
