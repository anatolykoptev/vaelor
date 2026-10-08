package main

import (
	"encoding/xml"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// xmlBuildingStatus is the review tools' analogue of the code_graph
// "building" response (issue #905): a normal result — never an error —
// telling the caller the repository call graph is warming in the
// background and to retry the same call shortly.
type xmlBuildingStatus struct {
	XMLName xml.Name `xml:"response"`
	Tool    string   `xml:"tool,attr"`
	Repo    string   `xml:"repo"`
	Status  string   `xml:"status"`
	Message string   `xml:"message"`
}

func deltaBuildingResult(tool, repo string) *mcp.CallToolResult {
	return textResult(xmlMarshalFragment(xmlBuildingStatus{
		Tool:    tool,
		Repo:    repo,
		Status:  "building",
		Message: "repository call graph is being built in the background — retry the same call in 1-3 minutes",
	}))
}

// gateDeltaCallGraph decides whether a DeltaReview call may proceed
// synchronously. The call-graph build inside DeltaReview is the
// dominating cost on a cold repository — it can hold the MCP request for
// minutes with no signal to the caller. When the target tree is the live
// checkout (analysisRoot == root), the cgCache key is stable, so a cold
// cache is answered with a building status plus a deduplicated background
// prefetch that converges the retry onto a warm cache.
//
// A head worktree (analysisRoot != root) is a per-call temp dir keyed by
// path: a background build on it could never converge (the worktree is
// removed when the call returns), so that path keeps the synchronous
// behaviour — bounded by the same cold build as before this gate existed.
func gateDeltaCallGraph(tool, repoArg, root, analysisRoot, language string) *mcp.CallToolResult {
	if analysisRoot != root {
		return nil
	}
	cgInput := callgraph.TraceRepoInput{Root: analysisRoot, Language: language}
	if callgraph.ReadyForBuild(cgInput) {
		return nil
	}
	callgraph.Prefetch(cgInput)
	return deltaBuildingResult(tool, repoArg)
}
