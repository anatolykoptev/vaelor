package codegraph

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Template parameter names referenced outside the template table.
const (
	paramName  = "name"
	paramFrom  = "from"
	paramTo    = "to"
	paramLimit = "limit"
)

// requiredTemplateParams are identity parameters a template cannot run
// without: an empty name renders as a match on the empty string, which finds
// nothing, and the caller gets a silently empty result instead of an error.
var requiredTemplateParams = map[string]bool{paramName: true, paramFrom: true, paramTo: true}

// maxTemplateLimit caps a {limit} parameter.
const maxTemplateLimit = 500

// ExplicitClassification validates a caller-chosen template and its params,
// letting code_graph run a template without asking the LLM to classify the
// query. It rejects freeform (which needs the LLM to write Cypher), unknown
// template IDs, params the template does not take (a typo would otherwise be
// dropped silently) and missing identity params.
func ExplicitClassification(id string, params map[string]string) (*Classification, error) {
	if id == templateFreeform {
		return nil, errors.New(`template "freeform" needs the LLM to write Cypher; omit template to use it`)
	}
	t := GetTemplate(id)
	if t == nil {
		return nil, fmt.Errorf("unknown template %q; valid templates: %s", id, TemplateSignatures())
	}
	takes := make(map[string]bool, len(t.Params))
	for _, p := range t.Params {
		takes[p] = true
	}
	out := make(map[string]string, len(params))
	for k, v := range params {
		if !takes[k] {
			return nil, fmt.Errorf("template %s does not take param %q; it takes %s", id, k, signature(t))
		}
		out[k] = v
	}
	for _, p := range t.Params {
		if requiredTemplateParams[p] && strings.TrimSpace(out[p]) == "" {
			return nil, fmt.Errorf("template %s requires param %q", id, p)
		}
	}
	return &Classification{Template: id, Params: out}, nil
}

// TemplateSignatures lists every template as id(params), sorted by ID —
// the compact form for tool descriptions and error hints.
func TemplateSignatures() string {
	ids := make([]string, 0, len(templates))
	for id := range templates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	sigs := make([]string, len(ids))
	for i, id := range ids {
		sigs[i] = id + signature(templates[id])
	}
	return strings.Join(sigs, ", ")
}

func signature(t *Template) string {
	return "(" + strings.Join(t.Params, ", ") + ")"
}

// sanitizeLimit returns v when it is a positive integer (capped at
// maxTemplateLimit), else the default. {limit} is rendered unquoted into
// `LIMIT {limit}`, so escaping alone does not keep a non-numeric value from
// changing the query.
func sanitizeLimit(v string) string {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return templateDefaults[paramLimit]
	}
	return strconv.Itoa(min(n, maxTemplateLimit))
}
