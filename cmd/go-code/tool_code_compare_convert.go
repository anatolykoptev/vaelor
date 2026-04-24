package main

import "github.com/anatolykoptev/go-code/internal/compare"

func buildCompareXML(r *compare.CompareResult) xmlCompareResponse {
	resp := xmlCompareResponse{
		Compare: xmlCompare{
			RepoA:          r.RepoA,
			RepoB:          r.RepoB,
			Query:          r.Query,
			MatchedSymbols: r.MatchedSymbols,
			UnmatchedA:     r.UnmatchedA,
			UnmatchedB:     r.UnmatchedB,
			MetricsA:       convertMetrics(r.MetricsA),
			MetricsB:       convertMetrics(r.MetricsB),
			MatchBreakdown: xmlMatchBreak{
				Exact:    r.MatchBreakdown.Exact,
				Modified: r.MatchBreakdown.Modified,
				Fuzzy:    r.MatchBreakdown.Fuzzy,
				Renamed:  r.MatchBreakdown.Renamed,
				Semantic: r.MatchBreakdown.Semantic,
				Moved:    r.MatchBreakdown.Moved,
			},
			ImportDiff: xmlImportDiff{
				CommonCount: r.ImportDiff.CommonCount,
				OnlyACount:  r.ImportDiff.OnlyACount,
				OnlyBCount:  r.ImportDiff.OnlyBCount,
				StdlibA:     r.ImportDiff.StdlibA,
				StdlibB:     r.ImportDiff.StdlibB,
				ExternalA:   r.ImportDiff.ExternalA,
				ExternalB:   r.ImportDiff.ExternalB,
				OnlyA:       r.ImportDiff.OnlyA,
				OnlyB:       r.ImportDiff.OnlyB,
				FrameworksA: r.ImportDiff.FrameworksA,
				FrameworksB: r.ImportDiff.FrameworksB,
			},
			Analysis: convertAnalysis(r.Analysis),
		},
	}

	if r.DiffStats != nil {
		resp.Compare.DiffStats = &xmlDiffStats{
			ModifiedWithDiff: r.DiffStats.ModifiedWithDiff,
			TotalInserts:     r.DiffStats.TotalInserts,
			TotalDeletes:     r.DiffStats.TotalDeletes,
			TotalUpdates:     r.DiffStats.TotalUpdates,
			TotalMoves:       r.DiffStats.TotalMoves,
		}
	}
	if len(r.HotspotsA) > 0 {
		resp.Compare.HotspotsA = convertHotspots(r.HotspotsA)
	}
	if len(r.HotspotsB) > 0 {
		resp.Compare.HotspotsB = convertHotspots(r.HotspotsB)
	}
	if r.RelStatsA != nil {
		resp.Compare.RelStatsA = &xmlRelStats{
			Total: r.RelStatsA.Total, Extends: r.RelStatsA.Extends,
			Implements: r.RelStatsA.Implements, Embeds: r.RelStatsA.Embeds,
			UniqueSubjects: r.RelStatsA.UniqueSubjects,
		}
	}
	if r.RelStatsB != nil {
		resp.Compare.RelStatsB = &xmlRelStats{
			Total: r.RelStatsB.Total, Extends: r.RelStatsB.Extends,
			Implements: r.RelStatsB.Implements, Embeds: r.RelStatsB.Embeds,
			UniqueSubjects: r.RelStatsB.UniqueSubjects,
		}
	}
	if r.QualityA != nil {
		resp.Compare.QualityA = &xmlQualityIndicators{
			TodoCount: r.QualityA.TodoCount, ErrorPatterns: r.QualityA.ErrorPatterns,
			PanicCount: r.QualityA.PanicCount, MagicNumbers: r.QualityA.MagicNumbers,
		}
	}
	if r.QualityB != nil {
		resp.Compare.QualityB = &xmlQualityIndicators{
			TodoCount: r.QualityB.TodoCount, ErrorPatterns: r.QualityB.ErrorPatterns,
			PanicCount: r.QualityB.PanicCount, MagicNumbers: r.QualityB.MagicNumbers,
		}
	}
	if r.FreshnessA != nil {
		resp.Compare.FreshnessA = &xmlFreshness{
			DepRatio: r.FreshnessA.DepFreshnessRatio, VulnRatio: r.FreshnessA.VulnSecurityRatio,
			TotalDeps: r.FreshnessA.TotalDeps, Outdated: r.FreshnessA.OutdatedDeps, Vulnerable: r.FreshnessA.VulnDeps,
		}
	}
	if r.FreshnessB != nil {
		resp.Compare.FreshnessB = &xmlFreshness{
			DepRatio: r.FreshnessB.DepFreshnessRatio, VulnRatio: r.FreshnessB.VulnSecurityRatio,
			TotalDeps: r.FreshnessB.TotalDeps, Outdated: r.FreshnessB.OutdatedDeps, Vulnerable: r.FreshnessB.VulnDeps,
		}
	}
	if r.DataflowA != nil {
		resp.Compare.DataflowA = &xmlCompareDataflow{
			DeadStores: r.DataflowA.DeadStores, UnusedVars: r.DataflowA.UnusedVars,
			TotalFindings: r.DataflowA.TotalFindings, FilesAnalyzed: r.DataflowA.FilesAnalyzed,
		}
	}
	if r.DataflowB != nil {
		resp.Compare.DataflowB = &xmlCompareDataflow{
			DeadStores: r.DataflowB.DeadStores, UnusedVars: r.DataflowB.UnusedVars,
			TotalFindings: r.DataflowB.TotalFindings, FilesAnalyzed: r.DataflowB.FilesAnalyzed,
		}
	}
	if r.APIDiffResult != nil {
		resp.Compare.APIDiff = convertAPIDiff(r.APIDiffResult)
	}
	if r.RouteDiffResult != nil {
		resp.Compare.RouteDiff = convertRouteDiff(r.RouteDiffResult)
	}
	if len(r.CouplingA) > 0 {
		resp.Compare.CouplingA = convertCoupling(r.CouplingA)
	}
	if len(r.CouplingB) > 0 {
		resp.Compare.CouplingB = convertCoupling(r.CouplingB)
	}
	if r.ArchMetricsA != nil {
		resp.Compare.ArchMetricsA = convertArchMetrics(r.ArchMetricsA)
	}
	if r.ArchMetricsB != nil {
		resp.Compare.ArchMetricsB = convertArchMetrics(r.ArchMetricsB)
	}
	if r.CrossLangReport != nil {
		resp.Compare.CrossLang = convertCrossLang(r.CrossLangReport)
	}

	return resp
}

func convertCoupling(pairs []compare.CoupledPair) *xmlCoupling {
	items := make([]xmlCoupledPair, len(pairs))
	for i, p := range pairs {
		items[i] = xmlCoupledPair{FileA: p.FileA, FileB: p.FileB, CoChanges: p.CoChanges, Ratio: p.Ratio}
	}
	return &xmlCoupling{Pairs: items}
}

func convertAPIDiff(d *compare.APIDiff) *xmlAPIDiff {
	x := &xmlAPIDiff{
		Common: d.Common, OnlyACount: d.OnlyACount,
		OnlyBCount: d.OnlyBCount, ChangedSig: d.ChangedSig,
	}
	for _, s := range d.OnlyA {
		x.OnlyA = append(x.OnlyA, xmlAPISymbol{Name: s.Name, Kind: s.Kind, Signature: s.Signature, Package: s.Package})
	}
	for _, s := range d.OnlyB {
		x.OnlyB = append(x.OnlyB, xmlAPISymbol{Name: s.Name, Kind: s.Kind, Signature: s.Signature, Package: s.Package})
	}
	for _, c := range d.Changed {
		x.Changed = append(x.Changed, xmlAPIDelta{Name: c.Name, Kind: c.Kind, SigA: c.SigA, SigB: c.SigB})
	}
	return x
}

func convertRouteDiff(d *compare.RouteDiff) *xmlRouteDiff {
	x := &xmlRouteDiff{
		Common: d.Common, OnlyACount: d.OnlyACount, OnlyBCount: d.OnlyBCount,
	}
	for _, r := range d.OnlyA {
		x.OnlyA = append(x.OnlyA, xmlRoute{Method: r.Method, Path: r.Path, Handler: r.Handler})
	}
	for _, r := range d.OnlyB {
		x.OnlyB = append(x.OnlyB, xmlRoute{Method: r.Method, Path: r.Path, Handler: r.Handler})
	}
	return x
}

func convertMetrics(m compare.RepoMetrics) xmlCompMetrics {
	return xmlCompMetrics{
		Files: m.Files, TotalLines: m.TotalLines,
		AvgFuncLines: m.AvgFuncLines, MaxFuncLines: m.MaxFuncLines,
		AvgComplexity: m.AvgComplexity, MaxComplexity: m.MaxComplexity,
		TestRatio: m.TestRatio, DocRatio: m.DocRatio,
		ErrorHandlingRatio: m.ErrorHandlingRatio,
		Interfaces:         m.Interfaces, ExternalDeps: m.ExternalDeps,
		Grade: m.Grade,

		AvgCognitiveComplexity: m.AvgCognitiveComplexity,
		MaxCognitiveComplexity: m.MaxCognitiveComplexity,
		AvgNestingDepth:        m.AvgNestingDepth,
		MaxNestingDepth:        m.MaxNestingDepth,
		LargeFileRatio:         m.LargeFileRatio,
		DuplicationRatio:       m.DuplicationRatio,
		MagicNumberRatio:       m.MagicNumberRatio,
		AvgParamCount:          m.AvgParamCount,
		MaxParamCount:          m.MaxParamCount,
		Score:                  m.Score,
		SemanticDupRatio:       m.SemanticDupRatio,
		DeadCodeCandidates:     m.DeadCodeCandidates,
	}
}

func convertAnalysis(a compare.LLMAnalysis) xmlAnalysis {
	xa := xmlAnalysis{Recommendations: a.Recommendations}
	for _, q := range a.Quality {
		xa.Quality = append(xa.Quality, xmlQuality{
			Aspect: q.Aspect, Winner: q.Winner, Reason: q.Reason,
			SnippetA: q.SnippetA, SnippetB: q.SnippetB,
		})
	}
	for _, g := range a.Gaps {
		xa.Gaps = append(xa.Gaps, xmlGap{
			MissingIn: g.MissingIn, Feature: g.Feature,
			Importance: g.Importance, Location: g.LocationB,
		})
	}
	for _, ai := range a.Architecture {
		xa.Architecture = append(xa.Architecture, xmlArchInsight{
			Insight: ai.Insight, Source: ai.Source,
			Example: ai.Example, Benefit: ai.Benefit,
		})
	}
	if a.Verdict.CanReplace != "" {
		xa.Verdict = &xmlVerdict{
			CanReplace: a.Verdict.CanReplace,
			Reason:     a.Verdict.Reason,
			Blockers:   a.Verdict.Blockers,
		}
	}
	return xa
}

func convertHotspots(hh []compare.HotspotFile) *xmlHotspots {
	items := make([]xmlHotspot, len(hh))
	for i, h := range hh {
		items[i] = xmlHotspot{
			File: h.File, Score: h.Score,
			Churn: h.Churn, Complexity: h.Complexity, Risk: h.Risk,
		}
	}
	return &xmlHotspots{Items: items}
}

func convertArchMetrics(m *compare.ArchMetrics) *xmlArchMetrics {
	x := &xmlArchMetrics{
		PackageCount:      m.PackageCount,
		CommunityCount:    m.CommunityCount,
		CrossPkgCallRatio: m.CrossPkgCallRatio,
		MaxCallDepth:      m.MaxCallDepth,
		InterfaceRatio:    m.InterfaceRatio,
		NotIndexed:        m.NotIndexed,
		Hint:              m.Hint,
	}
	for _, gp := range m.GodPackages {
		x.GodPackages = append(x.GodPackages, xmlGodPkg{Name: gp.Name, Importers: gp.Importers})
	}
	for _, cd := range m.CircularDeps {
		x.CircularDeps = append(x.CircularDeps, xmlCircDep{A: cd.PackageA, B: cd.PackageB})
	}
	return x
}

func convertCrossLang(r *compare.CrossLangReport) *xmlCrossLang {
	x := &xmlCrossLang{LangA: r.LanguageA, LangB: r.LanguageB, Matches: r.SemanticMatches}
	for _, m := range r.TopMatches {
		x.Top = append(x.Top, xmlCrossMatch{NameA: m.NameA, NameB: m.NameB, Sim: m.Similarity})
	}
	return x
}
