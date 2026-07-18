package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

// githubWebhookHandler validates the signature and enqueues events on a
// goroutine so long-running review + post does not block the delivery.
type githubWebhookHandler struct {
	secret []byte
	sink   func(event string, payload []byte)
}

func newGitHubWebhook(secret string, sink func(event string, payload []byte)) http.Handler {
	return &githubWebhookHandler{secret: []byte(secret), sink: sink}
}

func (h *githubWebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	sig := r.Header.Get("X-Hub-Signature-256")
	if event == "" || sig == "" {
		http.Error(w, "missing headers", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if !validHMAC(h.secret, body, sig) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	go h.sink(event, body)
	w.WriteHeader(http.StatusAccepted)
}

func validHMAC(secret, body []byte, sig string) bool {
	if !strings.HasPrefix(sig, "sha256=") {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(sig, "sha256="))
	if err != nil {
		return false
	}
	m := hmac.New(sha256.New, secret)
	m.Write(body)
	return hmac.Equal(m.Sum(nil), want)
}

// DispatchGitHubEvent routes a verified event to the right action.
// Called from the sink closure registered in main.go.
func DispatchGitHubEvent(event string, payload []byte, deps dispatchDeps) {
	switch event {
	case "pull_request":
		var p struct {
			Action string `json:"action"`
			Number int    `json:"number"`
			Repo   struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			PullRequest struct {
				User struct {
					Login string `json:"login"`
				} `json:"user"`
			} `json:"pull_request"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			log.Printf("webhook parse: %v", err)
			return
		}
		if p.Action != "opened" && p.Action != "synchronize" && p.Action != "reopened" {
			return
		}
		if deps.botUser != "" && p.PullRequest.User.Login == deps.botUser {
			return
		}
		if err := deps.postReview(p.Repo.FullName, p.Number); err != nil {
			log.Printf("post review %s#%d: %v", p.Repo.FullName, p.Number, err)
		}
	case "push":
		var p struct {
			Ref    string `json:"ref"`
			Before string `json:"before"`
			After  string `json:"after"`
			Repo   struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			Pusher struct {
				Name string `json:"name"`
			} `json:"pusher"`
			HeadCommit struct {
				Message string `json:"message"`
			} `json:"head_commit"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			log.Printf("webhook push parse: %v", err)
			return
		}
		if p.Ref != "refs/heads/main" {
			return
		}
		if deps.botUser != "" && p.Pusher.Name == deps.botUser {
			return
		}
		// Skip branch creation (before = 40 zeros) and deletion (after = 40 zeros).
		if strings.HasPrefix(p.Before, "00000000") || strings.HasPrefix(p.After, "00000000") {
			return
		}
		if deps.postPushReview == nil {
			return
		}
		if err := deps.postPushReview(p.Repo.FullName, p.Before, p.After); err != nil {
			log.Printf("post push review %s %s..%s: %v", p.Repo.FullName, p.Before[:8], p.After[:8], err)
		}
	case "issue_comment":
		// Stretch: @go-code mention dispatch (Task 8)
	}
}

type dispatchDeps struct {
	botUser        string
	postReview     func(slug string, pr int) error
	postPushReview func(slug, before, after string) error
}
