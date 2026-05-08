// internal/investigate/lifecycle.go
package investigate

import (
	"sync"
	"time"
)

// Status represents the lifecycle of an investigation.
type Status string

const (
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// State is one investigation's transient state.
type State struct {
	Status    Status
	StartedAt time.Time
	UpdatedAt time.Time
	Result    *InvestigationResult // populated when StatusDone
	Error     string               // populated when StatusFailed
}

// InvestigationStore deduplicates concurrent debug_investigate calls and
// stores results for polling. Key: service + range. Thread-safe.
type InvestigationStore struct {
	m sync.Map // map[string]*State
}

// NewInvestigationStore builds an empty store.
func NewInvestigationStore() *InvestigationStore {
	return &InvestigationStore{}
}

// Start either creates a new running investigation or returns the existing
// one. fresh=true on first call for this (service, range), false if already
// running or completed (dedup).
func (s *InvestigationStore) Start(service string, start, end time.Time) (*State, bool) {
	key := stateKey(service, start, end)
	st := &State{Status: StatusRunning, StartedAt: time.Now(), UpdatedAt: time.Now()}
	if existing, loaded := s.m.LoadOrStore(key, st); loaded {
		return existing.(*State), false
	}
	return st, true
}

// Finish marks the investigation done and stores the result.
func (s *InvestigationStore) Finish(service string, start, end time.Time, res *InvestigationResult) {
	key := stateKey(service, start, end)
	v, ok := s.m.Load(key)
	if !ok {
		return
	}
	st := v.(*State)
	st.Status = StatusDone
	st.UpdatedAt = time.Now()
	st.Result = res
}

// Fail marks the investigation failed with an error message.
func (s *InvestigationStore) Fail(service string, start, end time.Time, errMsg string) {
	key := stateKey(service, start, end)
	v, ok := s.m.Load(key)
	if !ok {
		return
	}
	st := v.(*State)
	st.Status = StatusFailed
	st.UpdatedAt = time.Now()
	st.Error = errMsg
}

// Get returns the State for (service, range) or (nil, false) if absent.
func (s *InvestigationStore) Get(service string, start, end time.Time) (*State, bool) {
	v, ok := s.m.Load(stateKey(service, start, end))
	if !ok {
		return nil, false
	}
	return v.(*State), true
}

// stateKey is the dedup key for the sync.Map.
func stateKey(service string, start, end time.Time) string {
	return service + "|" + start.UTC().Format(time.RFC3339) + "|" + end.UTC().Format(time.RFC3339)
}
