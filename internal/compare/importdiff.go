package compare

import "sort"

// ImportDiff captures the difference between two import sets.
type ImportDiff struct {
	CommonCount int      `json:"commonCount"`
	OnlyACount  int      `json:"onlyACount"`
	OnlyBCount  int      `json:"onlyBCount"`
	OnlyA       []string `json:"onlyA,omitempty"`
	OnlyB       []string `json:"onlyB,omitempty"`
	FrameworksA []string `json:"frameworksA,omitempty"`
	FrameworksB []string `json:"frameworksB,omitempty"`
	StdlibA     int      `json:"stdlibA"`
	StdlibB     int      `json:"stdlibB"`
	ExternalA   int      `json:"externalA"`
	ExternalB   int      `json:"externalB"`
}

// maxImportDiffItems limits how many items are listed in OnlyA/OnlyB.
const maxImportDiffItems = 30

// ComputeImportDiff computes the set difference between two import lists.
// The language parameter is used for categorization and framework detection.
func ComputeImportDiff(importsA, importsB []string, language string) ImportDiff {
	setA := make(map[string]struct{}, len(importsA))
	for _, imp := range importsA {
		setA[imp] = struct{}{}
	}

	setB := make(map[string]struct{}, len(importsB))
	for _, imp := range importsB {
		setB[imp] = struct{}{}
	}

	var common, onlyA, onlyB int
	var onlyAList, onlyBList []string

	for imp := range setA {
		if _, ok := setB[imp]; ok {
			common++
		} else {
			onlyA++
			if len(onlyAList) < maxImportDiffItems {
				onlyAList = append(onlyAList, imp)
			}
		}
	}

	for imp := range setB {
		if _, ok := setA[imp]; !ok {
			onlyB++
			if len(onlyBList) < maxImportDiffItems {
				onlyBList = append(onlyBList, imp)
			}
		}
	}

	sort.Strings(onlyAList)
	sort.Strings(onlyBList)

	// Categorize imports per repo.
	var stdlibA, externalA, stdlibB, externalB int
	for imp := range setA {
		switch CategorizeImport(imp, language) {
		case ImportStdlib:
			stdlibA++
		case ImportExternal:
			externalA++
		}
	}
	for imp := range setB {
		switch CategorizeImport(imp, language) {
		case ImportStdlib:
			stdlibB++
		case ImportExternal:
			externalB++
		}
	}

	return ImportDiff{
		CommonCount: common,
		OnlyACount:  onlyA,
		OnlyBCount:  onlyB,
		OnlyA:       onlyAList,
		OnlyB:       onlyBList,
		FrameworksA: DetectFrameworks(importsA, language),
		FrameworksB: DetectFrameworks(importsB, language),
		StdlibA:     stdlibA,
		StdlibB:     stdlibB,
		ExternalA:   externalA,
		ExternalB:   externalB,
	}
}
