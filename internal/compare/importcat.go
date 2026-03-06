package compare

import (
	"sort"
	"strings"
)

// ImportCategory classifies an import path.
type ImportCategory string

const (
	// ImportStdlib marks standard library imports.
	ImportStdlib ImportCategory = "stdlib"

	// ImportInternal marks project-relative (internal) imports.
	ImportInternal ImportCategory = "internal"

	// ImportExternal marks third-party/external imports.
	ImportExternal ImportCategory = "external"
)

// CategorizeImport classifies an import path as stdlib, internal, or external
// for the given language.
func CategorizeImport(imp, language string) ImportCategory {
	switch strings.ToLower(language) {
	case "go":
		return categorizeGoImport(imp)
	case "python":
		return categorizePythonImport(imp)
	case "javascript", "typescript":
		return categorizeJSImport(imp)
	case "cpp", "c":
		return categorizeCppImport(imp)
	default:
		return ImportExternal
	}
}

func categorizeGoImport(imp string) ImportCategory {
	// Go stdlib packages never contain a dot in the first path element.
	if !strings.Contains(imp, ".") {
		return ImportStdlib
	}
	return ImportExternal
}

func categorizePythonImport(imp string) ImportCategory {
	if strings.HasPrefix(imp, ".") {
		return ImportInternal
	}
	if pythonStdlib[imp] {
		return ImportStdlib
	}
	top := strings.SplitN(imp, ".", 2)[0]
	if pythonStdlib[top] {
		return ImportStdlib
	}
	return ImportExternal
}

func categorizeJSImport(imp string) ImportCategory {
	if strings.HasPrefix(imp, "./") || strings.HasPrefix(imp, "../") {
		return ImportInternal
	}
	if nodeBuiltins[imp] {
		return ImportStdlib
	}
	return ImportExternal
}

// pythonStdlib contains common Python standard library top-level module names.
var pythonStdlib = map[string]bool{
	"abc":             true,
	"argparse":        true,
	"ast":             true,
	"asyncio":         true,
	"base64":          true,
	"bisect":          true,
	"builtins":        true,
	"calendar":        true,
	"cmath":           true,
	"codecs":          true,
	"collections":     true,
	"concurrent":      true,
	"configparser":    true,
	"contextlib":      true,
	"copy":            true,
	"csv":             true,
	"ctypes":          true,
	"dataclasses":     true,
	"datetime":        true,
	"decimal":         true,
	"difflib":         true,
	"dis":             true,
	"email":           true,
	"enum":            true,
	"errno":           true,
	"faulthandler":    true,
	"fcntl":           true,
	"fileinput":       true,
	"fnmatch":         true,
	"fractions":       true,
	"ftplib":          true,
	"functools":       true,
	"gc":              true,
	"getpass":         true,
	"glob":            true,
	"gzip":            true,
	"hashlib":         true,
	"heapq":           true,
	"hmac":            true,
	"html":            true,
	"http":            true,
	"importlib":       true,
	"inspect":         true,
	"io":              true,
	"ipaddress":       true,
	"itertools":       true,
	"json":            true,
	"keyword":         true,
	"linecache":       true,
	"locale":          true,
	"logging":         true,
	"lzma":            true,
	"math":            true,
	"mimetypes":       true,
	"multiprocessing": true,
	"operator":        true,
	"os":              true,
	"pathlib":         true,
	"pdb":             true,
	"pickletools":     true,
	"platform":        true,
	"pprint":          true,
	"queue":           true,
	"random":          true,
	"re":              true,
	"readline":        true,
	"secrets":         true,
	"select":          true,
	"shelve":          true,
	"shlex":           true,
	"shutil":          true,
	"signal":          true,
	"site":            true,
	"socket":          true,
	"sqlite3":         true,
	"ssl":             true,
	"stat":            true,
	"string":          true,
	"struct":          true,
	"subprocess":      true,
	"sys":             true,
	"syslog":          true,
	"tempfile":        true,
	"textwrap":        true,
	"threading":       true,
	"time":            true,
	"timeit":          true,
	"token":           true,
	"tokenize":        true,
	"traceback":       true,
	"types":           true,
	"typing":          true,
	"unicodedata":     true,
	"unittest":        true,
	"urllib":          true,
	"uuid":            true,
	"venv":            true,
	"warnings":        true,
	"weakref":         true,
	"xml":             true,
	"zipfile":         true,
	"zipimport":       true,
	"zlib":            true,
}

// nodeBuiltins contains Node.js built-in module names.
var nodeBuiltins = map[string]bool{
	"assert":         true,
	"buffer":         true,
	"child_process":  true,
	"cluster":        true,
	"console":        true,
	"constants":      true,
	"crypto":         true,
	"dgram":          true,
	"dns":            true,
	"domain":         true,
	"events":         true,
	"fs":             true,
	"http":           true,
	"http2":          true,
	"https":          true,
	"module":         true,
	"net":            true,
	"os":             true,
	"path":           true,
	"perf_hooks":     true,
	"process":        true,
	"querystring":    true,
	"readline":       true,
	"repl":           true,
	"stream":         true,
	"string_decoder": true,
	"timers":         true,
	"tls":            true,
	"tty":            true,
	"url":            true,
	"util":           true,
	"v8":             true,
	"vm":             true,
	"worker_threads": true,
	"zlib":           true,
}

// frameworkPatterns maps language to patterns of "importPrefix:frameworkName".
var frameworkPatterns = map[string][]string{
	"go": {
		"github.com/gin-gonic/gin:gin",
		"github.com/labstack/echo:echo",
		"github.com/gofiber/fiber:fiber",
		"github.com/gorilla/mux:gorilla",
		"google.golang.org/grpc:grpc",
		"gorm.io/gorm:gorm",
		"github.com/jmoiron/sqlx:sqlx",
		"github.com/go-redis/redis:redis",
		"go.uber.org/zap:zap",
		"github.com/sirupsen/logrus:logrus",
		"github.com/stretchr/testify:testify",
		"github.com/spf13/cobra:cobra",
		"github.com/spf13/viper:viper",
	},
	"python": {
		"flask:flask",
		"django:django",
		"fastapi:fastapi",
		"requests:requests",
		"sqlalchemy:sqlalchemy",
		"celery:celery",
		"pytest:pytest",
		"numpy:numpy",
		"pandas:pandas",
		"torch:pytorch",
		"tensorflow:tensorflow",
	},
	"javascript": {
		"react:react",
		"express:express",
		"next:next.js",
		"vue:vue",
		"angular:angular",
		"axios:axios",
		"lodash:lodash",
		"jest:jest",
		"mocha:mocha",
	},
	"cpp": {
		"boost:boost",
		"Qt:qt",
		"grpc:grpc",
		"gtest:gtest",
		"opencv:opencv",
		"eigen:eigen",
		"nlohmann:nlohmann-json",
		"spdlog:spdlog",
		"fmt:fmt",
		"absl:abseil",
		"folly:folly",
		"Poco:poco",
	},
}

// DetectFrameworks returns a sorted list of framework names detected from the
// given import paths for the specified language.
func DetectFrameworks(imports []string, language string) []string {
	patterns, ok := frameworkPatterns[strings.ToLower(language)]
	if !ok {
		return nil
	}

	seen := make(map[string]bool)
	for _, imp := range imports {
		for _, pattern := range patterns {
			parts := strings.SplitN(pattern, ":", 2)
			prefix, name := parts[0], parts[1]
			if strings.HasPrefix(imp, prefix) && !seen[name] {
				seen[name] = true
			}
		}
	}

	if len(seen) == 0 {
		return nil
	}

	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}
