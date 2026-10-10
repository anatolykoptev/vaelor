package scip

import (
	"os"
	"path/filepath"
)

// Indexer binary names shared by the registry (detect.go) and the env rules.
const (
	indexerRustAnalyzer = "rust-analyzer"
	indexerScipJava     = "scip-java"
)

// envPassthrough lists the only variables an indexer child inherits from the
// parent for every indexer. Everything else — tokens, DSNs, service secrets,
// API keys — is dropped: the indexer (and anything it spawns, such as build
// scripts or the Python interpreter) is handed attacker-controlled repo
// content and must never see the server's credentials.
//
// Measured need (indexers run with `env -i` + these only):
//   - scip-typescript: PATH, HOME, LANG are enough.
//   - scip-python: PATH, HOME, LANG are enough.
//   - rust-analyzer: additionally CARGO_HOME and RUSTUP_HOME on rustup-managed
//     hosts (rustup refuses to pick a toolchain without them); system-package
//     installs only need the cargo registry cache location to avoid
//     re-downloading crates on every run.
var envPassthrough = []string{
	"PATH", "LANG", "LC_ALL", "LC_CTYPE", "TZ",
	// Public CA bundle locations, needed by cargo/coursier TLS. Not secrets.
	"SSL_CERT_FILE", "SSL_CERT_DIR",
}

// envPerIndexer lists extra variables a specific indexer needs for toolchain
// discovery and caches. Only paths and toolchain selectors, never credentials.
var envPerIndexer = map[string][]string{
	indexerRustAnalyzer: {"CARGO_HOME", "RUSTUP_HOME", "RUSTUP_TOOLCHAIN"},
	indexerScipJava:     {"JAVA_HOME", "COURSIER_CACHE"},
}

// indexerEnv builds the minimal environment for the named indexer. home is a
// per-run temporary directory used as HOME so nothing under the server's real
// home (credential helpers, ~/.netrc, ~/.ssh, ~/.config) is reachable via
// $HOME. realHome is the server's real home, used only to locate the shared
// cargo/rustup caches when CARGO_HOME / RUSTUP_HOME are not set explicitly.
func indexerEnv(name, home, realHome string, getenv func(string) string) []string {
	env := []string{"HOME=" + home}
	add := func(keys []string) {
		for _, k := range keys {
			if v := getenv(k); v != "" {
				env = append(env, k+"="+v)
			}
		}
	}
	add(envPassthrough)
	add(envPerIndexer[name])

	if name == indexerRustAnalyzer && realHome != "" {
		for _, c := range []struct{ key, dir string }{
			{"CARGO_HOME", ".cargo"},
			{"RUSTUP_HOME", ".rustup"},
		} {
			if getenv(c.key) != "" {
				continue
			}
			if p := filepath.Join(realHome, c.dir); dirExists(p) {
				env = append(env, c.key+"="+p)
			}
		}
	}
	return env
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
