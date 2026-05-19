package staircase

import (
	"math"
	"testing"
)

func TestTerminationAfterSixReversalsAndThresholdMean(t *testing.T) {
	t.Parallel()
	s := New()
	responses := []bool{true, true, false, true, true, false, true, true, false, true, true, false, true, true, false, true, true}
	for i, heard := range responses {
		if s.Done() {
			break
		}
		if err := s.Apply(heard); err != nil {
			t.Fatalf("Apply(%d) error = %v", i, err)
		}
	}
	if !s.Done() {
		t.Fatal("staircase should terminate after 6 reversals")
	}
	if s.ReversalCount() != 6 {
		t.Fatalf("reversal count = %d, want 6", s.ReversalCount())
	}
	if got := s.StepSize(); got != fineStep {
		t.Fatalf("step size = %v, want %v", got, fineStep)
	}
	result, err := s.Result()
	if err != nil {
		t.Fatalf("Result() error = %v", err)
	}
	if got, want := result.ReversalLevels, []float64{-25, -20, -22, -20, -22, -20}; len(got) != len(want) {
		t.Fatalf("reversals len = %d, want %d", len(got), len(want))
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("reversal[%d] = %v, want %v", i, got[i], want[i])
			}
		}
	}
	if got, want := result.ThresholdDBFS, (-20.0+-22.0+-20.0+-22.0)/4.0; got != want {
		t.Fatalf("threshold = %v, want %v", got, want)
	}
}

func TestAllHeardDoesNotReverse(t *testing.T) {
	t.Parallel()
	s := New()
	for i := 0; i < 10; i++ {
		if err := s.Apply(true); err != nil {
			t.Fatalf("Apply(true) error = %v", err)
		}
	}
	if s.ReversalCount() != 0 {
		t.Fatalf("reversal count = %d, want 0", s.ReversalCount())
	}
	if s.Level() >= DefaultStartLevel {
		t.Fatalf("level = %v, want lower than start", s.Level())
	}
}

func TestAllNotHeardDoesNotReverse(t *testing.T) {
	t.Parallel()
	s := New()
	for i := 0; i < 10; i++ {
		if err := s.Apply(false); err != nil {
			t.Fatalf("Apply(false) error = %v", err)
		}
	}
	if s.ReversalCount() != 0 {
		t.Fatalf("reversal count = %d, want 0", s.ReversalCount())
	}
	if s.Level() != 0 {
		t.Fatalf("level = %v, want clamped 0", s.Level())
	}
}

func TestOscillatingResponsesShrinkStepAtTwoReversals(t *testing.T) {
	t.Parallel()
	s := New()
	responses := []bool{true, true, false, true, true}
	for _, heard := range responses {
		if err := s.Apply(heard); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
	}
	if s.ReversalCount() != 2 {
		t.Fatalf("reversal count = %d, want 2", s.ReversalCount())
	}
	if math.Abs(s.StepSize()-fineStep) > 1e-9 {
		t.Fatalf("step size = %v, want %v", s.StepSize(), fineStep)
	}
}

func TestResultErrorsBeforeCompletion(t *testing.T) {
	t.Parallel()
	s := New()
	if _, err := s.Result(); err == nil {
		t.Fatal("Result() error = nil, want error before completion")
	}
}
