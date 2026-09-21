package apps

import "testing"

// TestDropBurstIgnoresASingleSpike is A-2b's repro: a host mostly flat
// all week, with one hour where a model pull drops 400 MB of free
// space, should not read as "filling" at that pull's rate — the pull is
// one step, not a trend.
func TestDropBurstIgnoresASingleSpike(t *testing.T) {
	const day = 1 << 30 // 1 GB, in "bytes" for this test's arithmetic
	ys := []float64{10 * day, 10 * day, 10 * day, 9.6 * day, 9.6 * day, 9.6 * day, 9.6 * day}
	out := dropBurst(ys)
	// The pull's one-time drop is undone, whichever level it lands the
	// line back on: what matters is that it reads flat (zero slope), not
	// which side of the step it settles at.
	for i, v := range out {
		if v != out[0] {
			t.Fatalf("dropBurst(%v) = %v, want a flat line (all equal); [%d] = %v differs from [0] = %v", ys, out, i, v, out[0])
		}
	}
}

// TestDropBurstLeavesARealTrendAlone: many small steps in the same
// direction are a trend, and dropBurst must not touch them — only one
// dominant jump ever qualifies as a burst.
func TestDropBurstLeavesARealTrendAlone(t *testing.T) {
	ys := []float64{100, 95, 90, 85, 80, 75, 70}
	out := dropBurst(ys)
	for i := range ys {
		if out[i] != ys[i] {
			t.Fatalf("dropBurst(%v) = %v, want it unchanged (a steady trend, not a burst)", ys, out)
		}
	}
}

func TestFillDaysNeverNegativeAndCapsAtTheWindow(t *testing.T) {
	// A trend so steep the naive division goes past zero: floored at 0.
	if d := fillDays(-1<<20, -1<<10, 7); d != 0 {
		t.Errorf("fillDays with already-negative free space = %v, want 0", d)
	}
	// A slow trend over a one-day window projects to "months," but the
	// estimate is capped at the week of history it came from.
	if d := fillDays(1<<40, -1<<10, 7); d != 7 {
		t.Errorf("fillDays should cap at the observed window (7): got %v", d)
	}
}

func TestFillSentenceNeverSaysZeroDays(t *testing.T) {
	got := fillSentence(1<<20, -2<<20, 7) // less than a day left
	if got == "" || contains(got, "0 days") {
		t.Errorf("fillSentence(%v) = %q, must not say 0 days", 1<<20, got)
	}
	if !contains(got, "today") {
		t.Errorf("fillSentence(%v) = %q, want it to say \"today\" under a day", 1<<20, got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
