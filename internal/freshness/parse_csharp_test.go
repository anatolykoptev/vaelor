package freshness

import (
	"testing"
)

func TestParseCsproj_Full(t *testing.T) {
	input := `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />
    <PackageReference Include="Serilog" Version="3.1.1" />
  </ItemGroup>
  <ItemGroup>
    <PackageReference Include="xunit" Version="2.6.0" />
  </ItemGroup>
</Project>`
	info := ParseCsproj([]byte(input))

	if info.Language != "csharp" {
		t.Errorf("Language = %q, want %q", info.Language, "csharp")
	}

	wantDeps := 3
	if len(info.Dependencies) != wantDeps {
		t.Fatalf("Dependencies count = %d, want %d", len(info.Dependencies), wantDeps)
	}

	if info.Dependencies[0].Name != "Newtonsoft.Json" {
		t.Errorf("dep name = %q", info.Dependencies[0].Name)
	}
	if info.Dependencies[0].Version != "13.0.3" {
		t.Errorf("dep version = %q", info.Dependencies[0].Version)
	}
}

func TestParseCsproj_Empty(t *testing.T) {
	input := `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
</Project>`
	info := ParseCsproj([]byte(input))
	if len(info.Dependencies) != 0 {
		t.Errorf("Dependencies count = %d, want 0", len(info.Dependencies))
	}
}

func TestParseCsproj_InvalidXML(t *testing.T) {
	info := ParseCsproj([]byte(`<broken`))
	if info.Language != "csharp" {
		t.Errorf("Language = %q, want %q", info.Language, "csharp")
	}
}
