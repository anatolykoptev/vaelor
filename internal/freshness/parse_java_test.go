package freshness

import (
	"testing"
)

func TestParsePomXML_Full(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <modelVersion>4.0.0</modelVersion>
  <dependencies>
    <dependency>
      <groupId>org.springframework</groupId>
      <artifactId>spring-core</artifactId>
      <version>6.1.0</version>
    </dependency>
    <dependency>
      <groupId>junit</groupId>
      <artifactId>junit</artifactId>
      <version>4.13.2</version>
    </dependency>
  </dependencies>
</project>`
	info := ParsePomXML([]byte(input))

	if info.Language != "java" {
		t.Errorf("Language = %q, want %q", info.Language, "java")
	}

	wantDeps := 2
	if len(info.Dependencies) != wantDeps {
		t.Fatalf("Dependencies count = %d, want %d", len(info.Dependencies), wantDeps)
	}

	if info.Dependencies[0].Name != "org.springframework:spring-core" {
		t.Errorf("dep name = %q", info.Dependencies[0].Name)
	}
	if info.Dependencies[0].Version != "6.1.0" {
		t.Errorf("dep version = %q", info.Dependencies[0].Version)
	}
}

func TestParsePomXML_NoDeps(t *testing.T) {
	input := `<project><modelVersion>4.0.0</modelVersion></project>`
	info := ParsePomXML([]byte(input))
	if len(info.Dependencies) != 0 {
		t.Errorf("Dependencies count = %d, want 0", len(info.Dependencies))
	}
}

func TestParsePomXML_InvalidXML(t *testing.T) {
	info := ParsePomXML([]byte(`<broken`))
	if info.Language != "java" {
		t.Errorf("Language = %q, want %q", info.Language, "java")
	}
}

func TestParsePomXML_NoVersion(t *testing.T) {
	input := `<project>
  <dependencies>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>lib</artifactId>
    </dependency>
  </dependencies>
</project>`
	info := ParsePomXML([]byte(input))
	if len(info.Dependencies) != 1 {
		t.Fatalf("Dependencies count = %d, want 1", len(info.Dependencies))
	}
	if info.Dependencies[0].Version != "" {
		t.Errorf("version = %q, want empty", info.Dependencies[0].Version)
	}
}
