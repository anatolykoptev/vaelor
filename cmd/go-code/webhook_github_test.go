package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateGitHubSignature(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "pull_request")

	rr := httptest.NewRecorder()
	h := newGitHubWebhook("s3cret", func(event string, payload []byte) {})
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		b, _ := io.ReadAll(rr.Body)
		t.Fatalf("want 202, got %d: %s", rr.Code, b)
	}
}

func TestValidateGitHubSignature_Bad(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	req.Header.Set("X-GitHub-Event", "pull_request")
	rr := httptest.NewRecorder()
	h := newGitHubWebhook("s3cret", func(event string, payload []byte) {})
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}
}
