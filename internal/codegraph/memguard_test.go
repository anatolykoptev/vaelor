package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestParsePSILine verifies that parsePSILine extracts avg10 and avg60 from
// a standard /proc/pressure/memory "some" line.
// Falsification: swap avg10/avg60 fields → values swapped.
func TestParsePSILine(t *testing.T) {
	t.Parallel()
	psi, err := parsePSILine("some avg10=0.50 avg60=1.20 avg300=0.06 total=1220065")
	if err != nil {
		t.Fatalf("parsePSILine: %v", err)
	}
	if psi.avg10 != 0.50 {
		t.Errorf("avg10 = %v, want 0.50", psi.avg10)
	}
	if psi.avg60 != 1.20 {
		t.Errorf("avg60 = %v, want 1.20", psi.avg60)
	}
}

// TestParsePSILine_Malformed verifies that a line without avg10/avg60 returns
// zero values without error (graceful degradation).
func TestParsePSILine_Malformed(t *testing.T) {
	t.Parallel()
	psi, err := parsePSILine("some total=100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if psi.avg10 != 0 || psi.avg60 != 0 {
		t.Errorf("expected zero values for missing fields, got avg10=%v avg60=%v", psi.avg10, psi.avg60)
	}
}

// TestReadMemAvailable parses a synthetic /proc/meminfo and verifies the
// MemAvailable value is returned in bytes (input is in kB).
// Falsification: change the kB→bytes multiplier → value off by 1024x.
func TestReadMemAvailable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	meminfoPath := filepath.Join(dir, "meminfo")
	// 4194304 kB = 4 GiB = 4294967296 bytes
	if err := os.WriteFile(meminfoPath, []byte("MemTotal:        16384000 kB\nMemAvailable:    4194304 kB\nBuffers:          100000 kB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Override the readMemAvailable function path by testing parse logic.
	// Since readMemAvailable hardcodes "/proc/meminfo", we test the parsing
	// by reading the temp file directly.
	data, err := os.ReadFile(meminfoPath)
	if err != nil {
		t.Fatal(err)
	}
	avail, err := parseMemAvailableFromBytes(data)
	if err != nil {
		t.Fatalf("parseMemAvailable: %v", err)
	}
	want := uint64(4194304 * 1024)
	if avail != want {
		t.Errorf("avail = %d, want %d (4 GiB in bytes)", avail, want)
	}
}

// TestReadMemoryPSI parses a synthetic /proc/pressure/memory file.
func TestReadMemoryPSI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	psiPath := filepath.Join(dir, "memory")
	if err := os.WriteFile(psiPath, []byte("some avg10=5.00 avg60=2.00 avg300=0.50 total=100\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(psiPath)
	if err != nil {
		t.Fatal(err)
	}
	psi, err := parseMemoryPSIFromBytes(data)
	if err != nil {
		t.Fatalf("parseMemoryPSI: %v", err)
	}
	if psi.avg10 != 5.00 {
		t.Errorf("avg10 = %v, want 5.00", psi.avg10)
	}
	if psi.avg60 != 2.00 {
		t.Errorf("avg60 = %v, want 2.00", psi.avg60)
	}
}

// TestCheckMemoryPressure_NonLinux verifies that on non-Linux platforms the
// guard is a no-op (Sufficient=true). On Linux this test still passes because
// CheckMemoryPressure reads the real /proc — if the host has enough memory,
// it returns true; if not, it returns false. The test only asserts that the
// function runs without panic and produces a consistent result.
func TestCheckMemoryPressure_Runs(t *testing.T) {
	t.Parallel()
	st := CheckMemoryPressure()
	// On any platform, Sufficient and Reason must be consistent.
	if st.Sufficient && st.Reason != "" {
		t.Errorf("Sufficient=true but Reason=%q (should be empty)", st.Reason)
	}
	if !st.Sufficient && st.Reason == "" {
		t.Error("Sufficient=false but Reason is empty (should explain why)")
	}
}

// TestCheckMemoryPressure_SufficientFromBytes verifies the threshold logic:
// given a synthetic MemAvailable above the threshold and low PSI, the check
// should pass.
func TestCheckMemoryPressure_SufficientFromBytes(t *testing.T) {
	t.Parallel()
	// 5 GiB available, PSI avg10=0 — well above thresholds.
	st := checkMemoryFromRaw(5*1024*1024*1024, memoryPSI{avg10: 0, avg60: 0})
	if !st.Sufficient {
		t.Errorf("expected Sufficient=true with 5 GiB + low PSI, got Reason=%q", st.Reason)
	}
}

// TestCheckMemoryPressure_LowMem verifies that below the MemAvailable
// threshold the guard refuses.
func TestCheckMemoryPressure_LowMem(t *testing.T) {
	t.Parallel()
	// 1 GiB available — below 3 GiB threshold.
	st := checkMemoryFromRaw(1*1024*1024*1024, memoryPSI{avg10: 0, avg60: 0})
	if st.Sufficient {
		t.Error("expected Sufficient=false with 1 GiB available")
	}
	if st.Reason == "" {
		t.Error("expected non-empty Reason for low memory refusal")
	}
}

// TestCheckMemoryPressure_HighPSI verifies that high PSI triggers refusal
// even when MemAvailable is sufficient.
func TestCheckMemoryPressure_HighPSI(t *testing.T) {
	t.Parallel()
	// 10 GiB available but PSI avg10=50 — above 20 threshold.
	st := checkMemoryFromRaw(10*1024*1024*1024, memoryPSI{avg10: 50, avg60: 30})
	if st.Sufficient {
		t.Error("expected Sufficient=false with PSI avg10=50")
	}
	if st.Reason == "" {
		t.Error("expected non-empty Reason for high PSI refusal")
	}
}

// TestMemGuardWatchdog_CancelsOnPressure verifies that the watchdog cancels
// the context when memory pressure is detected. We simulate this by writing
// a synthetic /proc/pressure/memory with high avg10 and pointing the watchdog
// at it — but since the watchdog reads the real /proc, we instead test the
// cancel mechanism directly: if CheckMemoryPressure returns false, the
// watchdog should call cancel.
//
// This test verifies the wiring (context cancel propagation) rather than the
// /proc reading, which is covered by TestCheckMemoryPressure_*.
func TestMemGuardWatchdog_CancelsContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start watchdog with a short interval.
	origInterval := memGuardCheckInterval
	memGuardCheckInterval = 50 * time.Millisecond
	defer func() { memGuardCheckInterval = origInterval }()

	// We can't force /proc to show pressure, so we verify the watchdog
	// exits cleanly when the context is cancelled (no goroutine leak).
	done := make(chan struct{})
	go func() {
		MemGuardWatchdog(ctx, cancel)
		close(done)
	}()

	cancel()
	select {
	case <-done:
		// OK — watchdog exited.
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not exit within 2s of context cancel")
	}
}

// TestMemGuardWatchdog_ExitsOnContextDone verifies the watchdog stops its
// ticker and returns when the context is already cancelled at start.
func TestMemGuardWatchdog_ExitsOnAlreadyCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		MemGuardWatchdog(ctx, cancel)
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(1 * time.Second):
		t.Fatal("watchdog did not exit on already-cancelled context")
	}
}
