package staircase

import (
	"errors"
	"fmt"
)

const (
	DefaultStartLevel = -20.0
	initialStep       = 5.0
	fineStep          = 2.0
	maxReversals      = 6
)

type Staircase struct {
	level            float64
	step             float64
	consecutiveHeard int
	lastDirection    int
	reversals        []float64
	trials           int
	responses        []bool
	terminated       bool
}

type Result struct {
	ThresholdDBFS  float64
	ReversalLevels []float64
	Trials         int
}

func New() *Staircase {
	return &Staircase{level: DefaultStartLevel, step: initialStep}
}

func (s *Staircase) Level() float64 {
	return s.level
}

func (s *Staircase) StepSize() float64 {
	return s.step
}

func (s *Staircase) ReversalCount() int {
	return len(s.reversals)
}

func (s *Staircase) Done() bool {
	return s.terminated
}

func (s *Staircase) Apply(heard bool) error {
	if s.terminated {
		return errors.New("staircase already terminated")
	}
	s.trials++
	s.responses = append(s.responses, heard)
	if heard {
		s.consecutiveHeard++
		if s.consecutiveHeard < 2 {
			return nil
		}
		s.consecutiveHeard = 0
		s.move(-1)
		return nil
	}
	s.consecutiveHeard = 0
	s.move(1)
	return nil
}

func (s *Staircase) Result() (Result, error) {
	if !s.terminated {
		return Result{}, errors.New("staircase is not complete")
	}
	if len(s.reversals) < 4 {
		return Result{}, fmt.Errorf("need at least 4 reversals, got %d", len(s.reversals))
	}
	start := len(s.reversals) - 4
	var sum float64
	for _, v := range s.reversals[start:] {
		sum += v
	}
	return Result{
		ThresholdDBFS:  sum / 4,
		ReversalLevels: append([]float64(nil), s.reversals...),
		Trials:         s.trials,
	}, nil
	}

func (s *Staircase) move(direction int) {
	if s.lastDirection != 0 && direction != s.lastDirection {
		s.reversals = append(s.reversals, s.level)
		if len(s.reversals) == 2 {
			s.step = fineStep
		}
		if len(s.reversals) >= maxReversals {
			s.terminated = true
			return
		}
	}
	s.lastDirection = direction
	s.level += float64(direction) * s.step
	if s.level > 0 {
		s.level = 0
	}
}
