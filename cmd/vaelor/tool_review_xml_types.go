package main

// xmlFlagHint is an optional child element emitted on a changed symbol when
// graph signals produced a non-empty flag. Omitted entirely on the cold path
// so output is byte-identical to pre-graph behaviour when no flags fire.
type xmlFlagHint struct {
	Kind string `xml:"kind,attr"`
	Note string `xml:"note,attr"`
}
