package freshness

import (
	"encoding/json"
)

// packageJSON represents the relevant fields of a package.json file.
type packageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Engines         struct {
		Node string `json:"node"`
	} `json:"engines"`
}

// ParsePackageJSON extracts dependencies from package.json content.
func ParsePackageJSON(data []byte) ManifestInfo {
	info := ManifestInfo{Language: "typescript"}

	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return info
	}

	info.RuntimeVersion = pkg.Engines.Node

	for name, version := range pkg.Dependencies {
		info.Dependencies = append(info.Dependencies, Dependency{
			Name:     name,
			Version:  version,
			Language: "typescript",
		})
	}

	for name, version := range pkg.DevDependencies {
		info.Dependencies = append(info.Dependencies, Dependency{
			Name:     name,
			Version:  version,
			Language: "typescript",
		})
	}

	return info
}
