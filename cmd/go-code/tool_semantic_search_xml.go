package main

import "encoding/xml"

// semantic_search response types migrated from hand-rolled fmt.Fprintf onto
// encoding/xml.Marshal (failure class: manual XML string-concatenation).
//
// The prior formatSemanticResults escaped its user-text nodes via escapeXML, so
// its output was already well-formed; well-formedness is now correct BY
// CONSTRUCTION. buildStatusResponse, by contrast, interpolated its
// <status>/<message> text with raw %s (no escaping) -- a latent gap now closed:
// xml.Marshal escapes those chardata nodes. Every message is a server-controlled
// constant/template with no XML-hostile characters, so the emitted bytes are
// unchanged for every real status/message (proven by the benign equivalence
// golden).
//
// Empty <results count="0"> serializes long-form <results ...></results>
// (xml.Marshal never self-closes), decoder-equivalent to the prior form. No
// xml.Header prolog is emitted -- these are fragments the MCP caller consumes.

type semanticRespXML struct {
	XMLName xml.Name           `xml:"response"`
	Tool    string             `xml:"tool,attr"`
	Query   string             `xml:"query"`
	Repo    string             `xml:"repo"`
	Results semanticResultsXML `xml:"results"`
}

type semanticResultsXML struct {
	Count   int                 `xml:"count,attr"`
	Results []semanticResultXML `xml:"result"`
}

// semanticResultXML models the two-shape <result>: the prior formatter emitted a
// pagerank attribute only when PageRank > 0, so PageRank is a pointer attr
// (present iff set), pre-formatted %.6f. Distance is likewise pre-formatted
// %.4f -- a raw float32 marshal uses strconv and drops trailing zeros
// (0.5000 -> 0.5), breaking attribute equivalence. rank/line are ints.
type semanticResultXML struct {
	Rank     int               `xml:"rank,attr"`
	Distance string            `xml:"distance,attr"`
	Source   string            `xml:"source,attr"`
	PageRank *string           `xml:"pagerank,attr,omitempty"`
	File     string            `xml:"file"`
	Symbol   semanticSymbolXML `xml:"symbol"`
	Line     int               `xml:"line"`
	Language string            `xml:"language"`
}

type semanticSymbolXML struct {
	Kind  string `xml:"kind,attr"`
	Value string `xml:",chardata"`
}

// semanticStatusXML is the disabled/indexing/not-indexed status shape shared by
// every buildStatusResponse caller.
type semanticStatusXML struct {
	XMLName xml.Name `xml:"response"`
	Tool    string   `xml:"tool,attr"`
	Query   string   `xml:"query"`
	Repo    string   `xml:"repo"`
	Status  string   `xml:"status"`
	Message string   `xml:"message"`
}
