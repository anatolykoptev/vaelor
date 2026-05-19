package main

import (
	"context"
	"errors"
	"testing"

	kitllm "github.com/anatolykoptev/go-kit/llm"
)

func TestClassifyOutcome(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, "ok"},
		{"unavailable", kitllm.ErrUnavailable, "unavailable"},
		{"circuit_open", kitllm.ErrCircuitOpen, "circuit_open"},
		{"401 auth", &kitllm.APIError{StatusCode: 401}, "auth"},
		{"403 auth", &kitllm.APIError{StatusCode: 403}, "auth"},
		{"429 rate_limit", &kitllm.APIError{StatusCode: 429}, "rate_limit"},
		{"500 server", &kitllm.APIError{StatusCode: 500}, "server"},
		{"502 server", &kitllm.APIError{StatusCode: 502}, "server"},
		{"400 client", &kitllm.APIError{StatusCode: 400}, "client"},
		{"404 client", &kitllm.APIError{StatusCode: 404}, "client"},
		{"generic error", errors.New("boom"), "error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyOutcome(c.err); got != c.want {
				t.Errorf("classifyOutcome(%v) = %q, want %q", c.err, got, c.want)
			}
		})
	}
}

func TestLLMMetricsMW_CallsNextAndPropagatesResult(t *testing.T) {
	var nextCalled bool
	wantResp := &kitllm.ChatResponse{}
	next := func(ctx context.Context, req *kitllm.ChatRequest) (*kitllm.ChatResponse, error) {
		nextCalled = true
		return wantResp, nil
	}

	resp, err := llmMetricsMW(context.Background(), &kitllm.ChatRequest{}, next)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !nextCalled {
		t.Fatal("middleware did not call next")
	}
	if resp != wantResp {
		t.Fatal("middleware did not return next's response")
	}
}

func TestLLMMetricsMW_PropagatesError(t *testing.T) {
	wantErr := errors.New("downstream failure")
	next := func(ctx context.Context, req *kitllm.ChatRequest) (*kitllm.ChatResponse, error) {
		return nil, wantErr
	}

	_, err := llmMetricsMW(context.Background(), &kitllm.ChatRequest{}, next)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}
