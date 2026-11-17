package gogenfilter

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-faster/yaml"
)

const (
	sqlcConfigFile    = "sqlc.yaml"
	sqlcConfigFileAlt = "sqlc.yml"
)

// sqlcV1Config represents a sqlc.yaml v1 configuration file structure.
type sqlcV1Config struct {
	Version  string          `yaml:"version"`
	Packages []sqlcV1Package `yaml:"packages"`
}

// sqlcV1Package represents a single package in a v1 sqlc.yaml config.
type sqlcV1Package struct {
	Path string `yaml:"path"` // output directory in v1 format

	OutputBatchFileName    string `yaml:"output_batch_file_name"`
	OutputDBFileName       string `yaml:"output_db_file_name"`
	OutputModelsFileName   string `yaml:"output_models_file_name"`
	OutputQuerierFileName  string `yaml:"output_querier_file_name"`
	OutputCopyfromFileName string `yaml:"output_copyfrom_file_name"`
	OutputFilesSuffix      string `yaml:"output_files_suffix"`
}

// sqlcConfig represents a sqlc.yaml v2 configuration file structure.
type sqlcConfig struct {
	Version string       `yaml:"version"`
	SQL     []sqlcEngine `yaml:"sql"`
}

// sqlcEngine represents a single SQL engine configuration in sqlc.yaml.
type sqlcEngine struct {
	Schema  string        `yaml:"schema"`
	Engine  string        `yaml:"engine"`
	Gen     sqlcGenConfig `yaml:"gen"`
	Codegen []sqlcCodegen `yaml:"codegen"`
}

// sqlcGenConfig represents the generation configuration in sqlc.yaml.
type sqlcGenConfig struct {
	Go   *sqlcGoConfig   `yaml:"go"`
	JSON *sqlcJSONConfig `yaml:"json"`
}

// sqlcGoConfig represents the Go-specific generation configuration.
// The output_*_file_name fields customize the fixed output filenames that sqlc
// writes into the configured output directory (see sqlc gen.go: db.go, models.go,
// querier.go, batch.go, copyfrom.go). Empty means sqlc's default name.
// output_files_suffix is appended to query file names only (e.g. query.sql_gen.go).
type sqlcGoConfig struct {
	Package string `yaml:"package"`
	Out     string `yaml:"out"`

	OutputBatchFileName    string `yaml:"output_batch_file_name"`
	OutputDBFileName       string `yaml:"output_db_file_name"`
	OutputModelsFileName   string `yaml:"output_models_file_name"`
	OutputQuerierFileName  string `yaml:"output_querier_file_name"`
	OutputCopyfromFileName string `yaml:"output_copyfrom_file_name"`
	OutputFilesSuffix      string `yaml:"output_files_suffix"`
}

// sqlcJSONConfig represents the JSON generation configuration.
type sqlcJSONConfig struct {
	Out string `yaml:"out"`
}

// sqlcCodegen represents a plugin-based codegen entry in sqlc.yaml.
type sqlcCodegen struct {
	Out    string `yaml:"out"`
	Plugin string `yaml:"plugin"`
}

// newSQLCConfigError creates a new SQLCConfigError with consistent formatting.
func newSQLCConfigError(
	code ErrorCode,
	configPath string,
	operation SQLCOperation,
	message string,
	err error,
) *SQLCConfigError {
	return &SQLCConfigError{
		Code:       code,
		ConfigPath: configPath,
		Operation:  operation,
		Message:    message,
		Err:        err,
	}
}

func sqlcFindError(path string, err error) *SQLCConfigError {
	return newSQLCConfigError(
		CodeSQLCConfigFind,
		path,
		OpSQLCFind,
		fmt.Sprintf("finding sqlc configs in %q", path),
		err,
	)
}

func sqlcWalkError(path string, err error) *SQLCConfigError {
	return newSQLCConfigError(
		CodeSQLCConfigWalk,
		path,
		OpSQLCWalk,
		fmt.Sprintf("walking %q for sqlc configs", path),
		err,
	)
}

func sqlcReadError(configPath string, err error) *SQLCConfigError {
	return newSQLCConfigError(
		CodeSQLCConfigRead,
		configPath,
		OpSQLCRead,
		"reading sqlc config",
		err,
	)
}

func sqlcCollectError(configPath string, err error) *SQLCConfigError {
	return newSQLCConfigError(
		CodeSQLCConfigCollect,
		configPath,
		OpSQLCCollect,
		"processing sqlc config",
		err,
	)
}

// FindSQLCConfigs searches for sqlc.yaml or sqlc.yml files in the given paths.
// Searches both the provided paths and their parent directories (up to 10 levels up).
// Returns a map of config file path to project root directory.
func FindSQLCConfigs(paths []string) (map[string]string, *SQLCConfigError) {
	configs := make(map[string]string)

	for _, path := range paths {
		err := findSQLCConfigsInPath(path, configs)
		if err != nil {
			return nil, err
		}
	}

	return configs, nil
}

// findSQLCConfigsInPath searches for sqlc configs in a single path.
func findSQLCConfigsInPath(path string, configs map[string]string) *SQLCConfigError {
	err := walkPathForSQLCConfigs(path, configs)
	if err != nil {
		return sqlcFindError(path, err)
	}

	findSQLCConfigsInParent(path, configs)

	return nil
}

// walkPathForSQLCConfigs walks a path to find sqlc config files.
func walkPathForSQLCConfigs(path string, configs map[string]string) *SQLCConfigError {
	err := filepath.WalkDir(path, func(filePath string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("accessing %q: %w", filePath, err)
		}

		if walkDirForSQLCConfigs(filePath, d.Name(), configs) {
			return filepath.SkipDir
		}

		return nil
	})
	if err != nil {
		return sqlcWalkError(path, err)
	}

	return nil
}

// walkDirForSQLCConfigs records sqlc config files during directory walking.
// Returns true if the directory should be skipped.
func walkDirForSQLCConfigs(filePath, dirName string, configs map[string]string) bool {
	if shouldSkipDirectory(dirName) {
		return true
	}

	recordSQLCConfig(filePath, configs)

	return false
}

// shouldSkipDirectory returns true if a directory should be skipped during walk.
func shouldSkipDirectory(name string) bool {
	if name == "." {
		return false
	}

	return strings.HasPrefix(name, ".") || name == nodeModulesDir || name == vendorDir
}

// recordSQLCConfig records a sqlc config file if it matches.
func recordSQLCConfig(filePath string, configs map[string]string) {
	filename := filepath.Base(filePath)
	if filename == sqlcConfigFile || filename == sqlcConfigFileAlt {
		configs[filePath] = filepath.Dir(filePath)
	}
}

// findSQLCConfigsInParent searches parent directories for sqlc config.
func findSQLCConfigsInParent(path string, configs map[string]string) {
	parentPath, err := FindProjectRoot(path, []string{sqlcConfigFile, sqlcConfigFileAlt})
	if err != nil || parentPath == "" {
		return
	}

	tryAddSQLCConfig(parentPath, sqlcConfigFile, configs)
	tryAddSQLCConfig(parentPath, sqlcConfigFileAlt, configs)
}

// tryAddSQLCConfig adds a config to the map if the file exists.
func tryAddSQLCConfig(parentPath, filename string, configs map[string]string) {
	configPath := filepath.Join(parentPath, filename)
	if fileExists(configPath) {
		configs[configPath] = parentPath
	}
}

// parseSQLCConfig reads and parses a sqlc.yaml file.
func parseSQLCConfig(configPath string) (*sqlcConfig, *SQLCConfigError) {
	data, err := os.ReadFile(configPath) //nolint:gosec // configPath is from controlled source
	if err != nil {
		return nil, sqlcReadError(configPath, err)
	}

	return unmarshalSQLCConfig(data, configPath)
}

func unmarshalSQLCYAML(data []byte, target any, configPath, errMsg string) *SQLCConfigError {
	err := yaml.Unmarshal(data, target)
	if err != nil {
		return newSQLCConfigError(
			CodeSQLCConfigParse,
			configPath,
			OpSQLCParse,
			errMsg,
			err,
		)
	}

	return nil
}

func unmarshalSQLCConfig(data []byte, configPath string) (*sqlcConfig, *SQLCConfigError) {
	var version struct {
		Version string `yaml:"version"`
	}

	err := unmarshalSQLCYAML(data, &version, configPath, "detecting sqlc config version")
	if err != nil {
		return nil, err
	}

	switch version.Version {
	case "1":
		return parseV1AsV2(data, configPath)
	case "2", "":
		var config sqlcConfig

		err := unmarshalSQLCYAML(data, &config, configPath, "parsing sqlc config")
		if err != nil {
			return nil, err
		}

		return &config, nil
	default:
		return nil, newSQLCConfigError(
			CodeSQLCConfigParse,
			configPath,
			OpSQLCParse,
			fmt.Sprintf("unsupported sqlc config version %q", version.Version),
			nil,
		)
	}
}

// parseV1AsV2 parses a v1 config and converts it to v2 format.
// v1 uses packages[].path as output dirs; v2 uses sql[].gen.go.out.
func parseV1AsV2(data []byte, configPath string) (*sqlcConfig, *SQLCConfigError) {
	var v1Config sqlcV1Config

	err := unmarshalSQLCYAML(data, &v1Config, configPath, "parsing sqlc v1 config")
	if err != nil {
		return nil, err
	}

	config := &sqlcConfig{Version: v1Config.Version, SQL: nil}

	for _, pkg := range v1Config.Packages {
		if pkg.Path != "" {
			config.SQL = append(config.SQL, sqlcEngine{
				Schema: "",
				Engine: "",
				Gen: sqlcGenConfig{Go: &sqlcGoConfig{
					Package:                "",
					Out:                    pkg.Path,
					OutputBatchFileName:    pkg.OutputBatchFileName,
					OutputDBFileName:       pkg.OutputDBFileName,
					OutputModelsFileName:   pkg.OutputModelsFileName,
					OutputQuerierFileName:  pkg.OutputQuerierFileName,
					OutputCopyfromFileName: pkg.OutputCopyfromFileName,
					OutputFilesSuffix:      pkg.OutputFilesSuffix,
				}, JSON: nil},
				Codegen: nil,
			})
		}
	}

	return config, nil
}

// Fixed output filenames sqlc writes into each configured output directory
// (see sqlc gen.go). They are configurable via the corresponding
// output_*_file_name config keys; the defaults listed here apply when the key
// is absent.
const (
	sqlcFileDB       = "db.go"
	sqlcFileModels   = "models.go"
	sqlcFileQuerier  = "querier.go"
	sqlcFileBatch    = "batch.go"
	sqlcFileCopyfrom = "copyfrom.go"
)

// sqlcDefaultFileNames is the ordered list of fixed output filenames. Order
// matters for test assertions.
//
//nolint:gochecknoglobals // immutable lookup table, not configurable state
var sqlcDefaultFileNames = []string{
	sqlcFileDB,
	sqlcFileModels,
	sqlcFileQuerier,
	sqlcFileBatch,
	sqlcFileCopyfrom,
}

// sqlcCustomFileName returns the config-customized filename for the given
// default name, or empty if not customized.
func sqlcCustomFileName(defaultName string, goCfg *sqlcGoConfig) string {
	switch defaultName {
	case sqlcFileDB:
		return goCfg.OutputDBFileName
	case sqlcFileModels:
		return goCfg.OutputModelsFileName
	case sqlcFileQuerier:
		return goCfg.OutputQuerierFileName
	case sqlcFileBatch:
		return goCfg.OutputBatchFileName
	case sqlcFileCopyfrom:
		return goCfg.OutputCopyfromFileName
	default:
		return ""
	}
}

// configuredSQLCFileNames returns the Go output filenames sqlc writes into a
// configured output dir, resolved from the config (empty → default). Per-query
// <source>.sql.go files are intentionally excluded — they are detected by the
// generic filename pattern, not by config membership.
func configuredSQLCFileNames(goCfg *sqlcGoConfig) []string {
	if goCfg == nil {
		return nil
	}

	names := make([]string, 0, len(sqlcDefaultFileNames))

	for _, def := range sqlcDefaultFileNames {
		if custom := sqlcCustomFileName(def, goCfg); custom != "" {
			names = append(names, custom)
		} else {
			names = append(names, filepath.Base(def))
		}
	}

	return names
}

// extractOutputDirs extracts output directories from a sqlc config's SQL engines.
func extractOutputDirs(config *sqlcConfig, projectRoot string) []string {
	var outputDirs []string

	for _, sqlEngine := range config.SQL {
		// Go output: sql[].gen.go.out
		if sqlEngine.Gen.Go != nil && sqlEngine.Gen.Go.Out != "" {
			outDir := filepath.Join(projectRoot, sqlEngine.Gen.Go.Out)
			outDir = filepath.Clean(outDir)
			outputDirs = append(outputDirs, outDir)
		}

		// JSON output: sql[].gen.json.out
		if sqlEngine.Gen.JSON != nil && sqlEngine.Gen.JSON.Out != "" {
			outDir := filepath.Join(projectRoot, sqlEngine.Gen.JSON.Out)
			outDir = filepath.Clean(outDir)
			outputDirs = append(outputDirs, outDir)
		}

		// Plugin codegen: sql[].codegen[].out
		for _, cg := range sqlEngine.Codegen {
			if cg.Out != "" {
				outDir := filepath.Join(projectRoot, cg.Out)
				outDir = filepath.Clean(outDir)
				outputDirs = append(outputDirs, outDir)
			}
		}
	}

	return outputDirs
}

// GetSQLOutputDirs returns a list of output directories from sqlc configuration files.
func GetSQLOutputDirs(paths []string) ([]string, *SQLCConfigError) {
	configPaths, err := FindSQLCConfigs(paths)
	if err != nil {
		return nil, err
	}

	var outputDirs []string

	for configPath, projectRoot := range configPaths {
		config, err := parseSQLCConfig(configPath)
		if err != nil {
			return nil, sqlcCollectError(configPath, err)
		}

		outputDirs = append(outputDirs, extractOutputDirs(config, projectRoot)...)
	}

	return outputDirs, nil
}

// sqlcDerivedConfig is the distilled, detection-relevant view of all sqlc
// config(s) in a project. It maps each declared output directory (cleaned,
// slash-separated, relative to the fs root) to the set of filenames sqlc writes
// there — the fixed defaults plus any output_*_file_name customizations.
// It is built once per Filter and cached.
type sqlcDerivedConfig struct {
	// dirFiles: output dir → accepted base filenames (including `.sql.go`
	// per-query names when the dir is a sqlc output dir).
	dirFiles map[string]map[string]bool
}

// sqlcDerivedConfigForFS builds the derived config from all sqlc config files
// reachable in fsys. Paths that don't exist or can't be parsed are skipped;
// a nil (not empty) error means no usable sqlc config was found. Returns the
// derived config and nil even when no configs exist (empty map).
func sqlcDerivedConfigForFS(fsys fs.FS, paths []string) (*sqlcDerivedConfig, *SQLCConfigError) {
	configPaths, err := FindSQLCConfigsFS(fsys, paths)
	if err != nil {
		return nil, err
	}

	derived := &sqlcDerivedConfig{dirFiles: make(map[string]map[string]bool)}

	for configPath := range configPaths {
		config, cfgErr := parseSQLCConfigFS(fsys, configPath)
		if cfgErr != nil {
			return nil, sqlcCollectError(configPath, cfgErr)
		}

		mergeSQLCConfig(derived, config)
	}

	return derived, nil
}

// mergeSQLCConfig folds one sqlc config into the derived config. For each
// Go output dir it records the fixed filenames (defaults + custom names) and
// the per-query ".sql.go" name.
func mergeSQLCConfig(derived *sqlcDerivedConfig, config *sqlcConfig) {
	if config == nil {
		return
	}

	for i := range config.SQL {
		engine := &config.SQL[i]

		goCfg := engine.Gen.Go
		if goCfg == nil || goCfg.Out == "" {
			continue
		}

		outDir := filepath.ToSlash(filepath.Clean(goCfg.Out))

		names := derived.dirFiles[outDir]
		if names == nil {
			names = make(map[string]bool)
			derived.dirFiles[outDir] = names
		}

		for _, name := range configuredSQLCFileNames(goCfg) {
			names[filepath.Base(name)] = true
		}
	}
}

// FindSQLCConfigsFS searches for sqlc.yaml or sqlc.yml files using the provided filesystem.
// Paths must be valid within fsys (relative to the FS root).
// Unlike FindSQLCConfigs, this does not search parent directories.
func FindSQLCConfigsFS(fsys fs.FS, paths []string) (map[string]string, *SQLCConfigError) {
	configs := make(map[string]string)

	for _, path := range paths {
		err := fs.WalkDir(fsys, path, func(filePath string, d fs.DirEntry, err error) error {
			if err != nil {
				return fmt.Errorf("accessing %q: %w", filePath, err)
			}

			if walkDirForSQLCConfigs(filePath, d.Name(), configs) {
				return fs.SkipDir
			}

			return nil
		})
		if err != nil {
			return nil, sqlcWalkError(path, err)
		}
	}

	return configs, nil
}

// parseSQLCConfigFS reads and parses a sqlc.yaml file from the given filesystem.
func parseSQLCConfigFS(fsys fs.FS, configPath string) (*sqlcConfig, *SQLCConfigError) {
	data, err := fs.ReadFile(fsys, configPath)
	if err != nil {
		return nil, sqlcReadError(configPath, err)
	}

	return unmarshalSQLCConfig(data, configPath)
}

// GetSQLOutputDirsFS returns output directories from sqlc configs using the provided filesystem.
// Paths must be valid within fsys (relative to the FS root).
func GetSQLOutputDirsFS(fsys fs.FS, paths []string) ([]string, *SQLCConfigError) {
	configPaths, err := FindSQLCConfigsFS(fsys, paths)
	if err != nil {
		return nil, err
	}

	var outputDirs []string

	for configPath, projectRoot := range configPaths {
		config, cfgErr := parseSQLCConfigFS(fsys, configPath)
		if cfgErr != nil {
			return nil, sqlcCollectError(configPath, cfgErr)
		}

		outputDirs = append(outputDirs, extractOutputDirs(config, projectRoot)...)
	}

	return outputDirs, nil
}
