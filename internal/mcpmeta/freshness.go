package mcpmeta

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LiveHead returns the on-disk HEAD SHA for a git repo without spawning git.
//
// It walks: .git/HEAD → (if symbolic) the refs/* file → (if missing)
// .git/packed-refs. Returns "" + error if none resolve.
func LiveHead(repoRoot string) (string, error) {
	headPath := filepath.Join(repoRoot, ".git", "HEAD")
	headBytes, err := os.ReadFile(headPath)
	if err != nil {
		return "", fmt.Errorf("read HEAD: %w", err)
	}
	head := strings.TrimSpace(string(headBytes))

	if len(head) == 40 && isHex(head) {
		return head, nil
	}

	if !strings.HasPrefix(head, "ref: ") {
		return "", fmt.Errorf("unrecognised HEAD: %q", head)
	}
	ref := strings.TrimPrefix(head, "ref: ")

	loose := filepath.Join(repoRoot, ".git", filepath.FromSlash(ref))
	if data, err := os.ReadFile(loose); err == nil {
		sha := strings.TrimSpace(string(data))
		if len(sha) == 40 && isHex(sha) {
			return sha, nil
		}
	}

	packed := filepath.Join(repoRoot, ".git", "packed-refs")
	f, err := os.Open(packed)
	if err != nil {
		return "", fmt.Errorf("ref not loose and no packed-refs: %w", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 && parts[1] == ref && len(parts[0]) == 40 && isHex(parts[0]) {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("ref %q not in packed-refs", ref)
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// WithFreshness annotates an envelope with staleness fields when the
// caller-supplied indexedSHA does not match the on-disk HEAD.
//
// Silent on match: silence is the calibrated signal.
func WithFreshness(env Envelope, repoRoot, indexedSHA string) Envelope {
	live, err := LiveHead(repoRoot)
	if err != nil || live == "" || indexedSHA == "" {
		return env
	}
	if live == indexedSHA {
		return env
	}
	env.StaleWarning = fmt.Sprintf(
		"index built against %s, live HEAD is %s — call code_graph refresh=true",
		short(indexedSHA), short(live),
	)
	env.IndexedSHA = indexedSHA
	env.LiveSHA = live
	return env
}

func short(sha string) string {
	if len(sha) < 7 {
		return sha
	}
	return sha[:7]
}
