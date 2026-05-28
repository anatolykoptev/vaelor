package compare

import (
	"testing"
)

func TestCategorizeImport_Go(t *testing.T) {
	tests := []struct {
		imp  string
		want ImportCategory
	}{
		{"fmt", ImportStdlib},
		{"net/http", ImportStdlib},
		{"os", ImportStdlib},
		{"context", ImportStdlib},
		{"encoding/json", ImportStdlib},
		{"github.com/gin-gonic/gin", ImportExternal},
		{"github.com/stretchr/testify/assert", ImportExternal},
		{"go.uber.org/zap", ImportExternal},
	}

	for _, tt := range tests {
		t.Run(tt.imp, func(t *testing.T) {
			got := CategorizeImport(tt.imp, "go")
			if got != tt.want {
				t.Errorf("CategorizeImport(%q, \"go\") = %q, want %q", tt.imp, got, tt.want)
			}
		})
	}
}

func TestCategorizeImport_Python(t *testing.T) {
	tests := []struct {
		imp  string
		want ImportCategory
	}{
		{"os", ImportStdlib},
		{"sys", ImportStdlib},
		{"json", ImportStdlib},
		{"os.path", ImportStdlib},
		{"collections.abc", ImportStdlib},
		{"requests", ImportExternal},
		{"flask", ImportExternal},
		{"numpy", ImportExternal},
		{".utils", ImportInternal},
		{"..models", ImportInternal},
		{".helpers.common", ImportInternal},
	}

	for _, tt := range tests {
		t.Run(tt.imp, func(t *testing.T) {
			got := CategorizeImport(tt.imp, "python")
			if got != tt.want {
				t.Errorf("CategorizeImport(%q, \"python\") = %q, want %q", tt.imp, got, tt.want)
			}
		})
	}
}

func TestCategorizeImport_JavaScript(t *testing.T) {
	tests := []struct {
		imp  string
		want ImportCategory
	}{
		{"fs", ImportStdlib},
		{"path", ImportStdlib},
		{"http", ImportStdlib},
		{"crypto", ImportStdlib},
		{"react", ImportExternal},
		{"express", ImportExternal},
		{"lodash", ImportExternal},
		{"./utils", ImportInternal},
		{"../models/user", ImportInternal},
		{"./components/Button", ImportInternal},
	}

	for _, tt := range tests {
		t.Run(tt.imp, func(t *testing.T) {
			got := CategorizeImport(tt.imp, "javascript")
			if got != tt.want {
				t.Errorf("CategorizeImport(%q, \"javascript\") = %q, want %q", tt.imp, got, tt.want)
			}
		})
	}
}

func TestCategorizeImport_TypeScript(t *testing.T) {
	// TypeScript should behave the same as JavaScript.
	got := CategorizeImport("fs", "typescript")
	if got != ImportStdlib {
		t.Errorf("CategorizeImport(\"fs\", \"typescript\") = %q, want %q", got, ImportStdlib)
	}

	got = CategorizeImport("./utils", "typescript")
	if got != ImportInternal {
		t.Errorf("CategorizeImport(\"./utils\", \"typescript\") = %q, want %q", got, ImportInternal)
	}
}

func TestCategorizeImport_UnknownLanguage(t *testing.T) {
	got := CategorizeImport("anything", "rust")
	if got != ImportExternal {
		t.Errorf("CategorizeImport for unknown language = %q, want %q", got, ImportExternal)
	}
}

// TestCategorizeImport_Kotlin verifies that Kotlin/Java stdlib prefixes are
// classified correctly (internal/compare/importcat.go).
func TestCategorizeImport_Kotlin(t *testing.T) {
	tests := []struct {
		imp  string
		want ImportCategory
	}{
		// Kotlin stdlib
		{"kotlin.collections.List", ImportStdlib},
		{"kotlin.text.StringBuilder", ImportStdlib},
		{"kotlinx.coroutines.launch", ImportStdlib},
		// Java stdlib (used from Kotlin)
		{"java.util.concurrent.Executor", ImportStdlib},
		{"javax.inject.Inject", ImportStdlib},
		// Android/AndroidX framework
		{"android.content.Context", ImportStdlib},
		{"androidx.lifecycle.ViewModel", ImportStdlib},
		// third-party
		{"com.squareup.okhttp3.OkHttpClient", ImportExternal},
		{"io.ktor.client.HttpClient", ImportExternal},
	}

	for _, tt := range tests {
		t.Run(tt.imp, func(t *testing.T) {
			got := CategorizeImport(tt.imp, "kotlin")
			if got != tt.want {
				t.Errorf("CategorizeImport(%q, \"kotlin\") = %q, want %q", tt.imp, got, tt.want)
			}
		})
	}
}

func TestDetectFrameworks(t *testing.T) {
	t.Run("go frameworks", func(t *testing.T) {
		imports := []string{
			"fmt",
			"net/http",
			"github.com/gin-gonic/gin",
			"github.com/go-redis/redis/v8",
			"gorm.io/gorm",
			"github.com/stretchr/testify/assert",
		}

		got := DetectFrameworks(imports, "go")
		want := []string{"gin", "gorm", "redis", "testify"}
		if len(got) != len(want) {
			t.Fatalf("DetectFrameworks() = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("DetectFrameworks()[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("python frameworks", func(t *testing.T) {
		imports := []string{"os", "flask", "sqlalchemy.orm"}

		got := DetectFrameworks(imports, "python")
		want := []string{"flask", "sqlalchemy"}
		if len(got) != len(want) {
			t.Fatalf("DetectFrameworks() = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("DetectFrameworks()[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("javascript frameworks", func(t *testing.T) {
		imports := []string{"react", "axios", "fs", "./utils"}

		got := DetectFrameworks(imports, "javascript")
		want := []string{"axios", "react"}
		if len(got) != len(want) {
			t.Fatalf("DetectFrameworks() = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("DetectFrameworks()[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("no frameworks found", func(t *testing.T) {
		imports := []string{"fmt", "os", "net/http"}
		got := DetectFrameworks(imports, "go")
		if got != nil {
			t.Errorf("DetectFrameworks() = %v, want nil", got)
		}
	})

	t.Run("unknown language", func(t *testing.T) {
		got := DetectFrameworks([]string{"anything"}, "rust")
		if got != nil {
			t.Errorf("DetectFrameworks() = %v, want nil", got)
		}
	})
}
