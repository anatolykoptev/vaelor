package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/sourcemap"
)

const resolveTestMap = `{"version":3,"sources":["src/app.svelte"],"names":["onMount"],"mappings":"AAAA,SAASA,SAAS","file":"app.js"}`

func newTestResolver() *sourcemap.Resolver {
	return sourcemap.NewResolver(http.DefaultClient, 10, 5*time.Minute)
}

func TestResolveHTTPHandler_200(t *testing.T) {
	// Serve a fake source map.
	mapSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app.js.map" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(resolveTestMap))
			return
		}
		http.NotFound(w, r)
	}))
	defer mapSrv.Close()

	resolver := newTestResolver()
	host := mapSrv.Listener.Addr().String()
	handler := resolveHTTPHandler([]string{host}, resolver)

	body, _ := json.Marshal(map[string]interface{}{
		"url":    mapSrv.URL + "/app.js",
		"line":   1,
		"column": 9,
	})
	req := httptest.NewRequest(http.MethodPost, "/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var frame sourcemap.Frame
	if err := json.NewDecoder(w.Body).Decode(&frame); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if frame.File != "src/app.svelte" || frame.Function != "onMount" {
		t.Errorf("unexpected frame: %+v", frame)
	}
}

func TestResolveHTTPHandler_502_MissingMap(t *testing.T) {
	// Server returns 404 for any map.
	mapSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer mapSrv.Close()

	host := mapSrv.Listener.Addr().String()
	resolver := newTestResolver()
	handler := resolveHTTPHandler([]string{host}, resolver)

	body, _ := json.Marshal(map[string]interface{}{
		"url":    mapSrv.URL + "/missing.js",
		"line":   1,
		"column": 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestResolveHTTPHandler_403_DisallowedHost(t *testing.T) {
	resolver := newTestResolver()
	handler := resolveHTTPHandler([]string{"allowed.example.com"}, resolver)

	body, _ := json.Marshal(map[string]interface{}{
		"url":    "https://evil.com/app.js",
		"line":   1,
		"column": 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestResolveHTTPHandler_400_BadJSON(t *testing.T) {
	resolver := newTestResolver()
	handler := resolveHTTPHandler([]string{"example.com"}, resolver)

	req := httptest.NewRequest(http.MethodPost, "/resolve", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestResolveHTTPHandler_405_GET(t *testing.T) {
	resolver := newTestResolver()
	handler := resolveHTTPHandler([]string{"example.com"}, resolver)

	req := httptest.NewRequest(http.MethodGet, "/resolve", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}
