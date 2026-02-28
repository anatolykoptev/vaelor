package retry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDo_Success(t *testing.T) {
	calls := 0
	result, err := Do(context.Background(), Options{MaxAttempts: 3}, func() (string, error) {
		calls++
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Fatalf("got result=%q, want %q", result, "ok")
	}
	if calls != 1 {
		t.Fatalf("got calls=%d, want 1", calls)
	}
}

func TestDo_RetriesThenSucceeds(t *testing.T) {
	calls := 0
	sentinel := errors.New("transient")

	result, err := Do(context.Background(), Options{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	}, func() (int, error) {
		calls++
		if calls < 3 {
			return 0, sentinel
		}
		return 42, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 42 {
		t.Fatalf("got result=%d, want 42", result)
	}
	if calls != 3 {
		t.Fatalf("got calls=%d, want 3", calls)
	}
}

func TestDo_ExhaustedReturnsLastError(t *testing.T) {
	calls := 0
	lastErr := errors.New("persistent failure")

	_, err := Do(context.Background(), Options{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	}, func() (struct{}, error) {
		calls++
		return struct{}{}, lastErr
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, lastErr) {
		t.Fatalf("got err=%v, want %v", err, lastErr)
	}
	if calls != 3 {
		t.Fatalf("got calls=%d, want 3", calls)
	}
}

func TestDo_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	calls := 0
	_, err := Do(ctx, Options{MaxAttempts: 3}, func() (string, error) {
		calls++
		return "", errors.New("should not reach")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got err=%v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("got calls=%d, want 0 (context was already cancelled)", calls)
	}
}

func TestHTTP_Retries429(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resp, err := HTTP(context.Background(), Options{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	}, func() (*http.Response, error) {
		return http.Get(srv.URL) //nolint:noctx // test-only, context not needed
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status=%d, want 200", resp.StatusCode)
	}
	if calls != 3 {
		t.Fatalf("got calls=%d, want 3", calls)
	}
}
