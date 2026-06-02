package freshness

import (
	"encoding/xml"
)

const langJava = "java"

// pomProject represents the relevant fields of a Maven pom.xml file.
type pomProject struct {
	XMLName      xml.Name      `xml:"project"`
	Dependencies pomDepSection `xml:"dependencies"`
}

// pomDepSection wraps the list of dependencies.
type pomDepSection struct {
	Deps []pomDep `xml:"dependency"`
}

// pomDep represents a single Maven dependency element.
type pomDep struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
}

// ParsePomXML extracts dependencies from Maven pom.xml content.
func ParsePomXML(data []byte) ManifestInfo {
	info := ManifestInfo{Language: langJava}

	var project pomProject
	if err := xml.Unmarshal(data, &project); err != nil {
		return info
	}

	for _, dep := range project.Dependencies.Deps {
		name := dep.GroupID + ":" + dep.ArtifactID
		info.Dependencies = append(info.Dependencies, Dependency{
			Name:     name,
			Version:  dep.Version,
			Language: langJava,
		})
	}

	return info
}
