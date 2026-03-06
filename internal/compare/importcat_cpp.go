package compare

import "strings"

// cppStdHeaders contains C++ standard library header names (without angle brackets).
var cppStdHeaders = map[string]bool{
	"algorithm": true, "any": true, "array": true, "atomic": true,
	"barrier": true, "bit": true, "bitset": true,
	"cassert": true, "cctype": true, "cerrno": true, "cfenv": true,
	"cfloat": true, "charconv": true, "chrono": true, "cinttypes": true,
	"climits": true, "clocale": true, "cmath": true, "codecvt": true,
	"compare": true, "complex": true, "concepts": true, "condition_variable": true,
	"coroutine": true, "csetjmp": true, "csignal": true, "cstdarg": true,
	"cstddef": true, "cstdint": true, "cstdio": true, "cstdlib": true,
	"cstring": true, "ctime": true, "cuchar": true, "cwchar": true,
	"cwctype": true, "deque": true, "exception": true, "execution": true,
	"expected": true, "filesystem": true, "format": true, "forward_list": true,
	"fstream": true, "functional": true, "future": true,
	"initializer_list": true, "iomanip": true, "ios": true,
	"iosfwd": true, "iostream": true, "istream": true, "iterator": true,
	"latch": true, "limits": true, "list": true, "locale": true,
	"map": true, "memory": true, "memory_resource": true, "mutex": true,
	"new": true, "numbers": true, "numeric": true, "optional": true,
	"ostream": true, "print": true, "queue": true, "random": true,
	"ranges": true, "ratio": true, "regex": true, "scoped_allocator": true,
	"semaphore": true, "set": true, "shared_mutex": true, "source_location": true,
	"span": true, "spanstream": true, "sstream": true, "stack": true,
	"stacktrace": true, "stdexcept": true, "stop_token": true, "streambuf": true,
	"string": true, "string_view": true, "syncstream": true, "system_error": true,
	"thread": true, "tuple": true, "type_traits": true, "typeindex": true,
	"typeinfo": true, "unordered_map": true, "unordered_set": true,
	"utility": true, "valarray": true, "variant": true, "vector": true,
	"version": true,
	// C compat headers
	"assert.h": true, "ctype.h": true, "errno.h": true, "float.h": true,
	"limits.h": true, "locale.h": true, "math.h": true, "setjmp.h": true,
	"signal.h": true, "stdarg.h": true, "stddef.h": true, "stdio.h": true,
	"stdlib.h": true, "string.h": true, "time.h": true,
	"stdint.h": true, "inttypes.h": true, "stdbool.h": true,
}

// categorizeCppImport classifies a C/C++ #include path.
func categorizeCppImport(imp string) ImportCategory {
	// Strip angle brackets or quotes.
	imp = strings.Trim(imp, "<>\"")

	// Local project includes (quoted paths).
	if strings.Contains(imp, "/") && !isCppThirdParty(imp) {
		return ImportInternal
	}

	// STL / C standard headers.
	if cppStdHeaders[imp] {
		return ImportStdlib
	}
	// Bare header without slash — check if it's a known STL name.
	base := strings.TrimSuffix(imp, ".h")
	if cppStdHeaders[base] {
		return ImportStdlib
	}

	return ImportExternal
}

// cppThirdPartyPrefixes are well-known third-party library include prefixes.
var cppThirdPartyPrefixes = []string{
	"boost/", "Qt", "grpc/", "grpc++/", "grpcpp/",
	"gtest/", "gmock/", "opencv", "Eigen/", "eigen3/",
	"nlohmann/", "spdlog/", "fmt/", "absl/",
	"folly/", "Poco/",
}

// isCppThirdParty checks if an include path matches a known third-party library.
func isCppThirdParty(imp string) bool {
	for _, prefix := range cppThirdPartyPrefixes {
		if strings.HasPrefix(imp, prefix) {
			return true
		}
	}
	return false
}
