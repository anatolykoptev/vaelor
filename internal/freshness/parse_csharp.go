package freshness

import (
	"encoding/xml"
)

const langCSharp = "csharp"

// csprojProject represents the relevant fields of a .csproj file.
type csprojProject struct {
	XMLName    xml.Name          `xml:"Project"`
	ItemGroups []csprojItemGroup `xml:"ItemGroup"`
}

// csprojItemGroup contains package references.
type csprojItemGroup struct {
	PackageRefs []csprojPackageRef `xml:"PackageReference"`
}

// csprojPackageRef represents a NuGet package reference.
type csprojPackageRef struct {
	Include string `xml:"Include,attr"`
	Version string `xml:"Version,attr"`
}

// ParseCsproj extracts dependencies from a .csproj file.
func ParseCsproj(data []byte) ManifestInfo {
	info := ManifestInfo{Language: langCSharp}

	var project csprojProject
	if err := xml.Unmarshal(data, &project); err != nil {
		return info
	}

	for _, group := range project.ItemGroups {
		for _, ref := range group.PackageRefs {
			if ref.Include == "" {
				continue
			}
			info.Dependencies = append(info.Dependencies, Dependency{
				Name:     ref.Include,
				Version:  ref.Version,
				Language: langCSharp,
			})
		}
	}

	return info
}
