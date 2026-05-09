// internal/investigate/lifecycle.go
package investigate

import (
	"sync"
	"time"
)

// Status represents the lifecycle of an investigation.
// Only the three exported consts (StatusRunning/Done/Failed) are valid;
// callers must not construct Status values directly.
type Status string

const (
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// State is one investigation's transient state.
// All field access goes through Lock/RLock — direct field reads from
// outside this package are not safe.
type State struct {
	mu        sync.RWMutex
	status    Status
	startedAt time.Time
	updatedAt time.Time
	result    *InvestigationResult
	errMsg    string
}

// Status returns the current lifecycle status.
func (s *State) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// Result returns the stored result (only valid when Status() == StatusDone).
func (s *State) Result() *InvestigationResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.result
}

// Error returns the failure message (only valid when Status() == StatusFailed).
func (s *State) Error() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.errMsg
}

// StartedAt returns when this investigation was created.
func (s *State) StartedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.startedAt
}

// UpdatedAt returns when this state was last mutated.
func (s *State) UpdatedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updatedAt
}

// stateKey is the dedup key for the sync.Map.
// Using a struct avoids "|" collision when service names contain that character.
// repo is included so that the same service+range with different repo args
// does not return a cached result from a prior (wrong-repo) investigation.
type stateKey struct {
	service string
	start   string // RFC3339 UTC
	end     string // RFC3339 UTC
	repo    string
}

// makeStateKey builds a collision-free key from the investigation parameters.
func makeStateKey(service string, start, end time.Time, repo string) stateKey {
	return stateKey{
		service: service,
		start:   start.UTC().Format(time.RFC3339),
		end:     end.UTC().Format(time.RFC3339),
		repo:    repo,
	}
}

// InvestigationStore deduplicates concurrent debug_investigate calls and
// stores results for polling. Key: service + range + repo. Thread-safe.
type InvestigationStore struct {
	m sync.Map // map[stateKey]*State
}

// NewInvestigationStore builds an empty store.
func NewInvestigationStore() *InvestigationStore {
	return &InvestigationStore{}
}

// Start either creates a new running investigation or returns the existing
// one. fresh=true on first call for this (service, range, repo), false if
// already running or completed (dedup).
func (s *InvestigationStore) Start(service string, start, end time.Time, repo string) (*State, bool) {
	key := makeStateKey(service, start, end, repo)
	now := time.Now()
	st := &State{status: StatusRunning, startedAt: now, updatedAt: now}
	if existing, loaded := s.m.LoadOrStore(key, st); loaded {
		return existing.(*State), false
	}
	return st, true
}

// Finish marks the investigation done and stores the result.
// Last writer wins when called concurrently.
func (s *InvestigationStore) Finish(service string, start, end time.Time, repo string, res *InvestigationResult) {
	key := makeStateKey(service, start, end, repo)
	v, ok := s.m.Load(key)
	if !ok {
		return
	}
	st := v.(*State)
	st.mu.Lock()
	st.status = StatusDone
	st.updatedAt = time.Now()
	st.result = res
	st.mu.Unlock()
}

// Fail marks the investigation failed with an error message.
// Last writer wins when called concurrently.
func (s *InvestigationStore) Fail(service string, start, end time.Time, repo string, errMsg string) {
	key := makeStateKey(service, start, end, repo)
	v, ok := s.m.Load(key)
	if !ok {
		return
	}
	st := v.(*State)
	st.mu.Lock()
	st.status = StatusFailed
	st.updatedAt = time.Now()
	st.errMsg = errMsg
	st.mu.Unlock()
}

// Get returns the State for (service, range, repo) or (nil, false) if absent.
func (s *InvestigationStore) Get(service string, start, end time.Time, repo string) (*State, bool) {
	v, ok := s.m.Load(makeStateKey(service, start, end, repo))
	if !ok {
		return nil, false
	}
	return v.(*State), true
}
