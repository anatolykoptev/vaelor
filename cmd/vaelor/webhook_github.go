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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
				AuthorAssociation string `json:"author_association"`
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
		// The review path fetches pull/N/head and builds a worktree from it, so
		// the PR head is attacker-controlled input. Review only PRs authored by
		// people the repo already trusts; everyone else (including a missing
		// field) is ignored. The 202 was already sent, so this is log + metric.
		if !trustedPRAuthor(p.PullRequest.AuthorAssociation) {
			webhookIgnoredTotal.WithLabelValues(webhookIgnoreUntrustedAuthor).Inc()
			log.Printf("webhook: ignoring %s#%d: author_association=%q not in OWNER/MEMBER/COLLABORATOR",
				p.Repo.FullName, p.Number, p.PullRequest.AuthorAssociation)
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

// webhookIgnoreUntrustedAuthor is the webhookIgnoredTotal reason for a
// pull_request whose author is not an owner, member or collaborator.
const webhookIgnoreUntrustedAuthor = "untrusted_author"

// webhookReasonLabel is the label name on webhookIgnoredTotal.
const webhookReasonLabel = "reason"

// webhookIgnoredTotal counts verified webhook events dropped by policy, by
// reason (untrusted_author). Dropping is silent to GitHub (the delivery got
// 202), so this counter is how an operator sees it happening.
var webhookIgnoredTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "vaelor_webhook_ignored_total",
		Help: "Verified GitHub webhook events ignored by policy, by reason (untrusted_author).",
	},
	[]string{webhookReasonLabel},
)

// trustedPRAuthor reports whether a pull_request author_association is one
// the repo vouches for. Anything else, including empty, is untrusted.
func trustedPRAuthor(association string) bool {
	switch association {
	case "OWNER", "MEMBER", "COLLABORATOR":
		return true
	}
	return false
}
