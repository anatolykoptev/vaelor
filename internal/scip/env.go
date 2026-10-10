package scip

import (
	"os"
	"path/filepath"
)

// Indexer binary names shared by the registry (detect.go) and the env rules.
const (
	indexerRustAnalyzer   = "rust-analyzer"
	indexerScipJava       = "scip-java"
	indexerScipTypescript = "scip-typescript"
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
// cargo/rustup/coursier caches when their *_HOME / *_CACHE vars are not set.
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

	// The throwaway HOME hides ~/.cargo and ~/.cache/coursier, so without an
	// explicit location every run would re-download the registry / jars.
	// These indexers only run on trusted roots (see AllowIndexer), so pointing
	// them at the server's shared caches is safe; the dirs are created on first
	// use by cargo/coursier.
	if name == indexerRustAnalyzer || name == indexerScipJava {
		// The image sets `safe.directory *` in the real HOME's .gitconfig, which
		// the throwaway HOME hides; restore it for these trusted-only runs via
		// command-scope config so git CLI calls on bind-mounted checkouts owned
		// by another uid do not fail with "dubious ownership".
		env = append(env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=safe.directory", "GIT_CONFIG_VALUE_0=*")
	}
	if realHome != "" {
		switch name {
		case indexerRustAnalyzer:
			setDefault(&env, getenv, "CARGO_HOME", filepath.Join(realHome, ".cargo"))
			if p := filepath.Join(realHome, ".rustup"); dirExists(p) {
				setDefault(&env, getenv, "RUSTUP_HOME", p)
			}
		case indexerScipJava:
			setDefault(&env, getenv, "COURSIER_CACHE", filepath.Join(realHome, ".cache", "coursier"))
		}
	}
	return env
}

// setDefault appends key=fallback unless the parent already set key (in which
// case add() already copied it).
func setDefault(env *[]string, getenv func(string) string, key, fallback string) {
	if getenv(key) == "" {
		*env = append(*env, key+"="+fallback)
	}
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
