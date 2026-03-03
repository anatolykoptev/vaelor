package main

import (
	"context"
	"encoding/xml"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/compare"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// xmlHealthResponse is the top-level XML envelope for code_health output.
type xmlHealthResponse struct {
	XMLName xml.Name  `xml:"response"`
	Health  xmlHealth `xml:"health"`
}

type xmlHealth struct {
	Repo            string              `xml:"repo,attr"`
	Language        string              `xml:"language,attr,omitempty"`
	Metrics         xmlCompMetrics      `xml:"metrics"`
	Score           float64             `xml:"score,attr"`
	Hotspots        *xmlHotspots        `xml:"hotspots,omitempty"`
	RelStats        *xmlRelStats        `xml:"relStats,omitempty"`
	Recommendations *xmlRecommendations `xml:"recommendations,omitempty"`
}

type xmlRecommendations struct {
	Items []xmlRecommendation `xml:"item"`
}

type xmlRecommendation struct {
	Priority  int    `xml:"priority,attr"`
	Potential string `xml:"potential,attr"`
	Area      string `xml:"area,attr"`
	Message   string `xml:",chardata"`
}

// CodeHealthInput is the input schema for the code_health tool.
type CodeHealthInput struct {
	Repo     string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`
	Language string `json:"language,omitempty" jsonschema_description:"Limit analysis to files of this language (e.g. go, python, rust)"`
	Focus    string `json:"focus,omitempty" jsonschema_description:"Subdirectory path to limit scope (e.g. internal/auth, pkg/api), or space-separated keywords (e.g. 'auth handler')"`
}

// registerCodeHealth registers the code_health MCP tool.
func registerCodeHealth(server *mcp.Server, cfg Config, deps analyze.Deps) {
	outputDir := cfg.OutputDir

	mcp.AddTool(server, &mcp.Tool{
		Name: "code_health",
		Description: "Assess code quality of a single repository. " +
			"Returns grade (A-F), numeric score (0-100), metrics " +
			"(complexity, test coverage, docs, error handling), " +
			"maintenance hotspots, type relationships, and " +
			"prioritized recommendations with estimated score impact. " +
			"No LLM — fast, purely static analysis.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeHealthInput) (*mcp.CallToolResult, any, error) {
		if input.Repo == "" {
			return errResult("repo is required"), nil, nil
		}

		root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo: %s", err)), nil, nil
		}
		defer cleanup()

		snap, err := compare.BuildSnapshot(ctx, root, compare.SnapshotOpts{
			Focus:    input.Focus,
			Language: input.Language,
		})
		if err != nil {
			return errResult(fmt.Sprintf("snapshot: %s", err)), nil, nil
		}

		metrics := compare.ComputeMetrics(snap)
		score := compare.GradeScore(metrics)

		// Hotspot analysis (non-fatal).
		churn, _ := compare.CollectChurn(ctx, root)
		var hotspots []compare.HotspotFile
		if churn != nil {
			hotspots = compare.ComputeHotspots(churn, compare.FileComplexityFromSnapshot(snap))
		}

		relStats := compare.ComputeRelStats(snap.Rels)

		// Recommendations.
		outliers := compare.CollectOutliers(snap)
		recs := compare.ComputeRecommendations(metrics, outliers, 5)

		resp := buildHealthXML(snap.Name, snap.Language, metrics, score, hotspots, relStats, recs)
		data, err := xml.MarshalIndent(resp, "", "  ")
		if err != nil {
			return errResult(fmt.Sprintf("marshal: %s", err)), nil, nil
		}
		return largeTextResult(xml.Header+string(data), "code_health", outputDir), nil, nil
	})
}

func buildHealthXML(
	name, language string,
	metrics compare.RepoMetrics,
	score float64,
	hotspots []compare.HotspotFile,
	relStats *compare.RelStats,
	recs []compare.Recommendation,
) xmlHealthResponse {
	resp := xmlHealthResponse{
		Health: xmlHealth{
			Repo:     name,
			Language: language,
			Metrics:  convertMetrics(metrics),
			Score:    score,
		},
	}

	if len(hotspots) > 0 {
		resp.Health.Hotspots = convertHotspots(hotspots)
	}
	if relStats != nil {
		resp.Health.RelStats = &xmlRelStats{
			Total: relStats.Total, Extends: relStats.Extends,
			Implements: relStats.Implements, Embeds: relStats.Embeds,
			UniqueSubjects: relStats.UniqueSubjects,
		}
	}
	if len(recs) > 0 {
		resp.Health.Recommendations = convertRecommendations(recs)
	}

	return resp
}

func convertRecommendations(recs []compare.Recommendation) *xmlRecommendations {
	items := make([]xmlRecommendation, len(recs))
	for i, r := range recs {
		items[i] = xmlRecommendation{
			Priority:  r.Priority,
			Potential: fmt.Sprintf("+%d", r.Potential),
			Area:      r.Area,
			Message:   r.Message,
		}
	}
	return &xmlRecommendations{Items: items}
}
