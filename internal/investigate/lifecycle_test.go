// internal/investigate/lifecycle_test.go
package investigate

import (
	"sync"
	"testing"
	"time"
)

func TestInvestigationStore_StartReturnsRunning(t *testing.T) {
	s := NewInvestigationStore()
	st, fresh := s.Start("svc", time.Unix(100, 0), time.Unix(200, 0))
	if !fresh {
		t.Error("expected fresh=true on first call")
	}
	if st.Status != StatusRunning {
		t.Errorf("expected running, got %q", st.Status)
	}
}

func TestInvestigationStore_StartDedupsSecondCall(t *testing.T) {
	s := NewInvestigationStore()
	_, fresh1 := s.Start("svc", time.Unix(100, 0), time.Unix(200, 0))
	_, fresh2 := s.Start("svc", time.Unix(100, 0), time.Unix(200, 0))
	if !fresh1 {
		t.Error("first call: expected fresh=true")
	}
	if fresh2 {
		t.Error("second call: expected fresh=false (dedup)")
	}
}

func TestInvestigationStore_FinishStoresResult(t *testing.T) {
	s := NewInvestigationStore()
	s.Start("svc", time.Unix(100, 0), time.Unix(200, 0))
	res := &InvestigationResult{}
	s.Finish("svc", time.Unix(100, 0), time.Unix(200, 0), res)

	st, ok := s.Get("svc", time.Unix(100, 0), time.Unix(200, 0))
	if !ok {
		t.Fatal("Get returned !ok after Finish")
	}
	if st.Status != StatusDone {
		t.Errorf("status: got %q, want done", st.Status)
	}
	if st.Result == nil {
		t.Error("result not stored")
	}
}

func TestInvestigationStore_FailMarksFailed(t *testing.T) {
	s := NewInvestigationStore()
	s.Start("svc", time.Unix(100, 0), time.Unix(200, 0))
	s.Fail("svc", time.Unix(100, 0), time.Unix(200, 0), "boom")

	st, _ := s.Get("svc", time.Unix(100, 0), time.Unix(200, 0))
	if st.Status != StatusFailed {
		t.Errorf("expected failed, got %q", st.Status)
	}
	if st.Error != "boom" {
		t.Errorf("expected error 'boom', got %q", st.Error)
	}
}

func TestInvestigationStore_DifferentRangeIsDifferentKey(t *testing.T) {
	s := NewInvestigationStore()
	_, fresh1 := s.Start("svc", time.Unix(100, 0), time.Unix(200, 0))
	_, fresh2 := s.Start("svc", time.Unix(300, 0), time.Unix(400, 0))
	if !fresh1 || !fresh2 {
		t.Errorf("different ranges should both be fresh; got %v %v", fresh1, fresh2)
	}
}

func TestInvestigationStore_ConcurrentStartIsRaceFree(t *testing.T) {
	s := NewInvestigationStore()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.Start("svc", time.Unix(100, 0), time.Unix(200, 0))
		}()
	}
	wg.Wait()
	// Run with -race.
}
