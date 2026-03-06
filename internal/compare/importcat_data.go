package compare

// pythonStdlib contains common Python standard library top-level module names.
var pythonStdlib = map[string]bool{
	"abc": true, "argparse": true, "ast": true, "asyncio": true,
	"base64": true, "bisect": true, "builtins": true,
	"calendar": true, "cmath": true, "codecs": true, "collections": true,
	"concurrent": true, "configparser": true, "contextlib": true,
	"copy": true, "csv": true, "ctypes": true,
	"dataclasses": true, "datetime": true, "decimal": true,
	"difflib": true, "dis": true,
	"email": true, "enum": true, "errno": true,
	"faulthandler": true, "fcntl": true, "fileinput": true,
	"fnmatch": true, "fractions": true, "ftplib": true, "functools": true,
	"gc": true, "getpass": true, "glob": true, "gzip": true,
	"hashlib": true, "heapq": true, "hmac": true, "html": true, "http": true,
	"importlib": true, "inspect": true, "io": true, "ipaddress": true, "itertools": true,
	"json": true, "keyword": true,
	"linecache": true, "locale": true, "logging": true, "lzma": true,
	"math": true, "mimetypes": true, "multiprocessing": true,
	"operator": true, "os": true,
	"pathlib": true, "pdb": true, "pickletools": true, "platform": true, "pprint": true,
	"queue": true, "random": true, "re": true, "readline": true,
	"secrets": true, "select": true, "shelve": true, "shlex": true,
	"shutil": true, "signal": true, "site": true, "socket": true,
	"sqlite3": true, "ssl": true, "stat": true, "string": true,
	"struct": true, "subprocess": true, "sys": true, "syslog": true,
	"tempfile": true, "textwrap": true, "threading": true,
	"time": true, "timeit": true, "token": true, "tokenize": true,
	"traceback": true, "types": true, "typing": true,
	"unicodedata": true, "unittest": true, "urllib": true, "uuid": true,
	"venv": true, "warnings": true, "weakref": true,
	"xml": true, "zipfile": true, "zipimport": true, "zlib": true,
}

// nodeBuiltins contains Node.js built-in module names.
var nodeBuiltins = map[string]bool{
	"assert": true, "buffer": true, "child_process": true,
	"cluster": true, "console": true, "constants": true, "crypto": true,
	"dgram": true, "dns": true, "domain": true, "events": true,
	"fs": true, "http": true, "http2": true, "https": true,
	"module": true, "net": true, "os": true, "path": true,
	"perf_hooks": true, "process": true, "querystring": true,
	"readline": true, "repl": true, "stream": true,
	"string_decoder": true, "timers": true, "tls": true, "tty": true,
	"url": true, "util": true, "v8": true, "vm": true,
	"worker_threads": true, "zlib": true,
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
		"flask:flask", "django:django", "fastapi:fastapi",
		"requests:requests", "sqlalchemy:sqlalchemy", "celery:celery",
		"pytest:pytest", "numpy:numpy", "pandas:pandas",
		"torch:pytorch", "tensorflow:tensorflow",
	},
	"javascript": {
		"react:react", "express:express", "next:next.js",
		"vue:vue", "angular:angular", "axios:axios",
		"lodash:lodash", "jest:jest", "mocha:mocha",
	},
	"cpp": {
		"boost:boost", "Qt:qt", "grpc:grpc", "gtest:gtest",
		"opencv:opencv", "eigen:eigen", "nlohmann:nlohmann-json",
		"spdlog:spdlog", "fmt:fmt", "absl:abseil",
		"folly:folly", "Poco:poco",
	},
}
