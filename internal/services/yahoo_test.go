package services

import (
	"math"
	"testing"
)

func TestComputeChange_PrefersPriorDailyClose(t *testing.T) {
	// Yahoo says today=$83.30, but the 5d series knows yesterday's close
	// was $83.10 (a +0.24% day). Yahoo's stale meta.prevClose of $98.00
	// would falsely paint this as a -15% crash.
	change, pct := computeChange(83.30, 83.10, 98.00)
	if math.Abs(change-0.20) > 1e-9 {
		t.Errorf("expected change ~0.20 from prior daily close, got %v", change)
	}
	wantPct := (0.20 / 83.10) * 100
	if math.Abs(pct-wantPct) > 1e-6 {
		t.Errorf("expected pct ~%.6f, got %v", wantPct, pct)
	}
}

func TestComputeChange_FallsBackToMetaPrevClose(t *testing.T) {
	// No prior daily close available (zero) → use Yahoo's meta value.
	change, pct := computeChange(100, 0, 95)
	if math.Abs(change-5) > 1e-9 {
		t.Errorf("expected change=5 using meta prevClose, got %v", change)
	}
	if math.Abs(pct-(5.0/95.0*100)) > 1e-6 {
		t.Errorf("unexpected pct: %v", pct)
	}
}

func TestComputeChange_SuppressesExtremeMoves(t *testing.T) {
	// Both inputs say -16.6% — that's almost certainly a Yahoo bug, so we
	// suppress to (0, 0) rather than render a misleading double-digit
	// number.
	change, pct := computeChange(83.30, 100, 0)
	if change != 0 || pct != 0 {
		t.Errorf("expected extreme move to be suppressed, got change=%v pct=%v", change, pct)
	}
}

func TestComputeChange_HandlesZeroAndNegativeInputs(t *testing.T) {
	cases := [][3]float64{
		{0, 80, 81},  // missing live price
		{80, 0, 0},   // no prevClose source at all
		{80, -5, -5}, // bogus negative prev close
	}
	for _, c := range cases {
		change, pct := computeChange(c[0], c[1], c[2])
		if change != 0 || pct != 0 {
			t.Errorf("expected (0,0) for invalid input %v, got change=%v pct=%v", c, change, pct)
		}
	}
}

func TestComputeChange_KeepsLargeButPlausibleMoves(t *testing.T) {
	// Within ±10% (e.g. an 8% oil-shock day) we keep the value.
	change, pct := computeChange(108, 100, 100)
	if change != 8 || math.Abs(pct-8) > 1e-9 {
		t.Errorf("expected 8%% move to pass the guard, got change=%v pct=%v", change, pct)
	}
}
