package main

import (
	"os"
	"strconv"

	"github.com/anatolykoptev/go-code/internal/workspace"
)

// autoIndexDirs returns the (optionally translated) directories to scan for
// auto-indexing. It checks GO_CODE_AUTOINDEX_TRANSLATE: when "true", it
// applies PATH_MAPPINGS to cfg.AutoIndexDirs via workspace.TranslateDirs so
// host-side paths become container-internal paths. Default is false — existing
// behavior preserved until the operator opts in.
//
// Both the eager-warm path (main.go) and the embeddings auto-index path
// (register.go) call this helper so the env-check is not duplicated.
func autoIndexDirs(cfg Config) []string {
	if translateAuto, _ := strconv.ParseBool(os.Getenv(autoIndexTranslateEnv)); translateAuto {
		return workspace.TranslateDirs(cfg.AutoIndexDirs, cfg.PathMappings)
	}
	return cfg.AutoIndexDirs
}
