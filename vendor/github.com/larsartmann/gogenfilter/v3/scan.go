package gogenfilter

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// GeneratedFile holds information about a single detected generated file.
type GeneratedFile struct {
	Path   string       // relative path within the scanned directory
	Reason FilterReason // which generator was detected
}

// String returns a human-readable representation of the generated file.
func (f GeneratedFile) String() string {
	return f.Path + " (" + string(f.Reason) + ")"
}

// ScanResult holds the results of scanning a project for generated files.
type ScanResult struct {
	Files        []GeneratedFile     // all detected generated files
	ByGenerator  map[string][]string // generator name → list of file paths
	ScannedFiles int                 // total .go files scanned
	Generators   []string            // unique generator names, sorted
	Exclusions   []Exclusion         // derived exclusion patterns
}

// String returns a human-readable summary of the scan result.
func (r *ScanResult) String() string {
	return fmt.Sprintf("ScanResult(scanned=%d, generated=%d, generators=%v)",
		r.ScannedFiles, len(r.Files), r.Generators)
}

// Exclusion represents a path-based exclusion pattern for auto-generated code.
type Exclusion struct {
	Pattern string // regex pattern suitable for golangci-lint exclusions.paths
	Reason  string // human-readable reason for the exclusion
}

// String returns a human-readable representation of the exclusion.
func (e Exclusion) String() string {
	return e.Pattern + " # " + e.Reason
}

// Scan phase identifiers used in ScanError.Phase for diagnostic reporting.
const (
	scanPhaseConfigure    = "configure"
	scanPhaseCreateFilter = "create_filter"
	scanPhaseCollect      = "collect"
	scanPhaseWalk         = "walk"
)

// Generator exclusion pattern literals. Extracted to constants so the same
// pattern is reused for the lookup table and the reason string.
const (
	templExclusionPattern = `_templ\.go$`
	templExclusionReason  = "templ generated HTML components"
)

// exclusionPatterns maps FilterReason values to fixed regex patterns for generators
// with consistent filename conventions. Used by ExclusionPattern() and deriveExclusions().
// Generators not in this map need directory-based derivation (oapi-codegen, generic, etc.).
//
//nolint:gochecknoglobals // immutable lookup table, never mutated
var exclusionPatterns = map[FilterReason]string{
	ReasonSQLC:          `\.sql\.go$`,
	ReasonTempl:         templExclusionPattern,
	ReasonProtobuf:      `\.pb\.go$`,
	ReasonGoEnum:        `_enum\.go$`,
	ReasonDeepcopy:      `zz_generated\..*\.go$`,
	ReasonWire:          `wire_gen\.go$`,
	ReasonMoq:           `_moq\.go$`,
	ReasonMockgen:       `_mock\.go$`,
	ReasonStringer:      `_string\.go$`,
	ReasonMockery:       `mock_.*\.go$`,
	ReasonEasyjson:      `_easyjson\.go$`,
	ReasonMsgp:          `_gen\.go$`,
	ReasonCounterfeiter: `fake_.*\.go$`,
	ReasonOapi:          `\.gen\.go$`,
}

// ExclusionPattern returns a fixed regex pattern for generators that have
// consistent filename conventions. Returns ("", false) for generators
// that need directory-based derivation (oapi-codegen, ent, gqlgen, generic).
func (r FilterReason) ExclusionPattern() (string, bool) {
	p, ok := exclusionPatterns[r]

	return p, ok
}

// ScanProject scans a project directory for auto-generated Go files.
// It walks the filesystem, detects generated files using the provided options,
// and returns a ScanResult with per-generator file lists and derived exclusion patterns.
//
// Default options (FilterAll) are used when no configs are provided.
// Directories named vendor, node_modules, and hidden directories (starting with .)
// are skipped during scanning.
//
// The fsys parameter provides the filesystem to scan. Use os.DirFS(projectDir)
// for the real filesystem.
func ScanProject(fsys fs.FS, configs ...FilterConfig) (*ScanResult, error) {
	if len(configs) == 0 {
		opts, err := WithFilterOptions(FilterAll)
		if err != nil {
			return nil, &ScanError{Code: CodeScanConfig, Phase: scanPhaseConfigure, Err: err}
		}

		configs = []FilterConfig{opts}
	}

	configs = append(configs, WithFS(fsys))

	filter, err := NewFilter(configs...)
	if err != nil {
		return nil, &ScanError{Code: CodeScanConfig, Phase: scanPhaseCreateFilter, Err: err}
	}

	goFiles, err := collectGoFiles(fsys)
	if err != nil {
		return nil, &ScanError{Code: CodeScanWalk, Phase: scanPhaseCollect, Err: err}
	}

	byGenerator := make(map[string][]string)

	var files []GeneratedFile

	for _, file := range goFiles {
		result, filterErr := filter.FilterDetailed(file)
		if filterErr != nil {
			continue
		}

		if !result.Filtered || result.Reason == ReasonNotFiltered {
			continue
		}

		generator := string(result.Reason)
		byGenerator[generator] = append(byGenerator[generator], file)

		files = append(files, GeneratedFile{
			Path:   file,
			Reason: result.Reason,
		})
	}

	generators := make([]string, 0, len(byGenerator))
	for gen := range byGenerator {
		generators = append(generators, gen)
	}

	sort.Strings(generators)

	exclusions := deriveExclusions(byGenerator, goFiles)

	return &ScanResult{
		Files:        files,
		ByGenerator:  byGenerator,
		ScannedFiles: len(goFiles),
		Generators:   generators,
		Exclusions:   exclusions,
	}, nil
}

// collectGoFiles walks the filesystem and collects all .go file paths
// relative to the root, excluding vendor, node_modules, and hidden directories.
func collectGoFiles(fsys fs.FS) ([]string, error) {
	var goFiles []string

	err := fs.WalkDir(fsys, ".", func(path string, dirEntry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walking %s: %w", path, err)
		}

		if !dirEntry.IsDir() {
			if strings.HasSuffix(dirEntry.Name(), ".go") {
				goFiles = append(goFiles, path)
			}

			return nil
		}

		if path != "." && shouldSkipScanDir(dirEntry.Name()) {
			return fs.SkipDir
		}

		return nil
	})
	if err != nil {
		return nil, &ScanError{Code: CodeScanWalk, Phase: scanPhaseWalk, Err: err}
	}

	return goFiles, nil
}

func shouldSkipScanDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}

	return name == vendorDir || name == nodeModulesDir
}

// deriveExclusions converts detected files into exclusion patterns.
// Generators with fixed filename conventions use ExclusionPattern(), but only
// when the pattern matches every detected file of that generator. Others use
// scoped derivation: a directory pattern when the directory is fully generated,
// precise per-file patterns otherwise.
func deriveExclusions(byGenerator map[string][]string, allGoFiles []string) []Exclusion {
	detectedAll := make(map[string]struct{})

	for _, files := range byGenerator {
		for _, f := range files {
			detectedAll[f] = struct{}{}
		}
	}

	seen := make(map[string]struct{})

	exclusions := make([]Exclusion, 0, len(detectedAll))

	for generator, files := range byGenerator {
		for _, exclusion := range exclusionsForGenerator(generator, files, allGoFiles, detectedAll) {
			if _, dup := seen[exclusion.Pattern]; dup {
				continue
			}

			seen[exclusion.Pattern] = struct{}{}

			exclusions = append(exclusions, exclusion)
		}
	}

	sort.Slice(exclusions, func(i, j int) bool {
		return exclusions[i].Pattern < exclusions[j].Pattern
	})

	return exclusions
}

// generatorExclusionReasons returns a map of generator names to human-readable reasons.
// Returned as a function-local value so the lookup table does not need to be a package-level
// mutable global.
func generatorExclusionReasons() map[string]string {
	return map[string]string{
		string(FilterTempl):           templExclusionReason,
		string(FilterProtobuf):        "protobuf generated code",
		string(FilterGoEnum):          "go-enum generated enumerations",
		string(deepcopyGeneratorName): "deepcopy-gen generated code",
		string(FilterWire):            "wire generated dependency injection",
		string(moqGeneratorName):      "moq generated mocks",
		string(FilterMockgen):         "mockgen generated mocks",
		string(FilterStringer):        "stringer generated string methods",
		string(FilterMockery):         "mockery generated mocks",
		string(FilterEasyjson):        "easyjson generated marshalers",
		string(FilterMsgp):            "msgp generated serialization",
		string(FilterCounterfeiter):   "counterfeiter generated fakes",
		string(FilterSQLC):            "sqlc generated database code",
		string(FilterOapi):            "oapi-codegen generated API code",
	}
}

func exclusionsForGenerator(
	generator string,
	files []string,
	allGoFiles []string,
	detectedAll map[string]struct{},
) []Exclusion {
	reason := generatorExclusionReasons()[generator]
	if reason == "" {
		reason = "auto-generated code"
	}

	if pattern, ok := FilterReason(generator).ExclusionPattern(); ok && allFilesMatch(files, pattern) {
		return []Exclusion{{Pattern: pattern, Reason: reason}}
	}

	return scopedExclusions(files, allGoFiles, detectedAll, reason)
}

// scopedExclusions derives exclusion patterns for generators without a fixed
// filename convention covering all their detected files. A directory pattern
// is emitted only when every .go file under the directory (recursively) is
// detected as generated; mixed directories get one precise per-file pattern
// per detected file instead, so a single detected file can never blanket-exclude
// hand-written neighbors.
func scopedExclusions(
	files []string,
	allGoFiles []string,
	detectedAll map[string]struct{},
	reason string,
) []Exclusion {
	byDir := make(map[string][]string)

	for _, f := range files {
		dir := filepath.Dir(f)
		byDir[dir] = append(byDir[dir], f)
	}

	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}

	sort.Strings(dirs)

	var emittedDirs []string

	exclusions := make([]Exclusion, 0, len(files))

	for _, dir := range dirs {
		if hasEmittedAncestor(dir, emittedDirs) {
			continue
		}

		if dir != "." && dirFullyGenerated(dir, allGoFiles, detectedAll) {
			emittedDirs = append(emittedDirs, dir)

			exclusions = append(exclusions, Exclusion{
				Pattern: "^" + regexp.QuoteMeta(dir) + "/",
				Reason:  reason,
			})

			continue
		}

		dirFiles := byDir[dir]
		sort.Strings(dirFiles)

		for _, f := range dirFiles {
			exclusions = append(exclusions, Exclusion{
				Pattern: "^" + regexp.QuoteMeta(f) + "$",
				Reason:  reason,
			})
		}
	}

	return exclusions
}

// hasEmittedAncestor reports whether an already-emitted directory pattern
// covers dir as a subdirectory.
func hasEmittedAncestor(dir string, emittedDirs []string) bool {
	for _, emitted := range emittedDirs {
		if strings.HasPrefix(dir, emitted+"/") {
			return true
		}
	}

	return false
}

// dirFullyGenerated reports whether every .go file under dir (recursively)
// is in the detected-generated set.
func dirFullyGenerated(dir string, allGoFiles []string, detectedAll map[string]struct{}) bool {
	prefix := dir + "/"

	for _, f := range allGoFiles {
		if strings.HasPrefix(f, prefix) {
			if _, ok := detectedAll[f]; !ok {
				return false
			}
		}
	}

	return true
}

// allFilesMatch reports whether pattern matches every given file path.
func allFilesMatch(files []string, pattern string) bool {
	if len(files) == 0 {
		return false
	}

	matched := true

	for _, f := range files {
		fileMatched, matchErr := regexp.MatchString(pattern, f)
		if matchErr != nil || !fileMatched {
			matched = false

			break
		}
	}

	return matched
}

// ExclusionPaths extracts just the pattern strings from a slice of Exclusions.
func ExclusionPaths(exclusions []Exclusion) []string {
	paths := make([]string, 0, len(exclusions))
	for _, e := range exclusions {
		paths = append(paths, e.Pattern)
	}

	return paths
}
