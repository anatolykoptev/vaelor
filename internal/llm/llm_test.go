package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// successResponse returns a valid CompletionResponse JSON with the given content.
func successResponse(content string) []byte {
	resp := CompletionResponse{
		Choices: []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}{
			{Message: struct {
				Content string `json:"content"`
			}{Content: content}},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

func TestComplete_RetriesOn500(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(successResponse("ok"))
	}))
	defer srv.Close()

	c := NewClient(Config{
		BaseURL:    srv.URL,
		MaxRetries: 3,
	})

	result, err := c.Complete(t.Context(), "", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Fatalf("expected %q, got %q", "ok", result)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 calls, got %d", got)
	}
}

func TestComplete_FallbackKey(t *testing.T) {
	const primaryKey = "primary"
	const fallbackKey = "secondary"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "Bearer "+primaryKey {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// fallback key gets a success response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(successResponse("from-fallback"))
	}))
	defer srv.Close()

	c := NewClient(Config{
		BaseURL:      srv.URL,
		APIKey:       primaryKey,
		FallbackKeys: []string{fallbackKey},
		MaxRetries:   2,
	})

	result, err := c.Complete(t.Context(), "", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "from-fallback" {
		t.Fatalf("expected %q, got %q", "from-fallback", result)
	}
}

func TestCompleteRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(successResponse("raw-response"))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL})

	result, err := c.CompleteRaw(t.Context(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "raw-response" {
		t.Fatalf("expected %q, got %q", "raw-response", result)
	}
}

func TestNewClient_DefaultMaxRetries(t *testing.T) {
	c := NewClient(Config{BaseURL: "http://localhost"})
	if c.maxRetries != defaultMaxRetries {
		t.Errorf("expected default maxRetries %d, got %d", defaultMaxRetries, c.maxRetries)
	}
}
