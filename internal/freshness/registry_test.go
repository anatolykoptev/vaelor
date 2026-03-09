package freshness

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func TestGoRegistry_Latest(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/golang.org/x/text/@latest" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"Version":"v0.21.0"}`))
	})
	defer srv.Close()

	reg := &GoRegistry{BaseURL: srv.URL, Client: srv.Client()}
	v, err := reg.Latest(context.Background(), "golang.org/x/text")
	assertNoError(t, err)
	assertEqual(t, "v0.21.0", v)
}

func TestNpmRegistry_Latest(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/express/latest" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"version":"4.18.2"}`))
	})
	defer srv.Close()

	reg := &NpmRegistry{BaseURL: srv.URL, Client: srv.Client()}
	v, err := reg.Latest(context.Background(), "express")
	assertNoError(t, err)
	assertEqual(t, "4.18.2", v)
}

func TestPyPIRegistry_Latest(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pypi/requests/json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"info":{"version":"2.31.0"}}`))
	})
	defer srv.Close()

	reg := &PyPIRegistry{BaseURL: srv.URL, Client: srv.Client()}
	v, err := reg.Latest(context.Background(), "requests")
	assertNoError(t, err)
	assertEqual(t, "2.31.0", v)
}

func TestCratesRegistry_Latest(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/crates/serde" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"crate":{"max_stable_version":"1.0.197"}}`))
	})
	defer srv.Close()

	reg := &CratesRegistry{BaseURL: srv.URL, Client: srv.Client()}
	v, err := reg.Latest(context.Background(), "serde")
	assertNoError(t, err)
	assertEqual(t, "1.0.197", v)
}

func TestMavenRegistry_Latest(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/solrsearch/select" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"response":{"docs":[{"latestVersion":"6.1.4"}]}}`))
	})
	defer srv.Close()

	reg := &MavenRegistry{BaseURL: srv.URL, Client: srv.Client()}
	v, err := reg.Latest(context.Background(), "org.springframework:spring-core")
	assertNoError(t, err)
	assertEqual(t, "6.1.4", v)
}

func TestMavenRegistry_InvalidName(t *testing.T) {
	reg := &MavenRegistry{BaseURL: "http://unused", Client: http.DefaultClient}
	_, err := reg.Latest(context.Background(), "no-colon-here")
	if err == nil {
		t.Error("expected error for invalid Maven name format")
	}
}

func TestRubyGemsRegistry_Latest(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/gems/rails.json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"version":"7.1.3"}`))
	})
	defer srv.Close()

	reg := &RubyGemsRegistry{BaseURL: srv.URL, Client: srv.Client()}
	v, err := reg.Latest(context.Background(), "rails")
	assertNoError(t, err)
	assertEqual(t, "7.1.3", v)
}

func TestNuGetRegistry_Latest(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3-flatcontainer/newtonsoft.json/index.json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"versions":["12.0.0","13.0.1","13.0.3"]}`))
	})
	defer srv.Close()

	reg := &NuGetRegistry{BaseURL: srv.URL, Client: srv.Client()}
	v, err := reg.Latest(context.Background(), "Newtonsoft.Json")
	assertNoError(t, err)
	assertEqual(t, "13.0.3", v)
}

func TestMultiRegistry_ForLanguage(t *testing.T) {
	mr := NewMultiRegistry(http.DefaultClient)

	languages := []string{"go", "npm", "python", "rust", "java", "ruby", "csharp"}
	for _, lang := range languages {
		if mr.ForLanguage(lang) == nil {
			t.Errorf("ForLanguage(%q) returned nil", lang)
		}
	}

	if mr.ForLanguage("unknown") != nil {
		t.Error("ForLanguage(unknown) should return nil")
	}
}
