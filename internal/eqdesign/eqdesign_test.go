package eqdesign

import (
	"math"
	"testing"
	"time"

	"hearing-eq/internal/profile"
)

func TestBuildAppliesHalfGainCapAndGlobalAttenuation(t *testing.T) {
	t.Parallel()
	p := &profile.Profile{
		Version:             profile.Version,
		CreatedAt:           time.Now().UTC(),
		FrequenciesHz:       []float64{80, 200, 500, 1000},
		LeftThresholdsDBFS:  []float64{-60, -54, -40, -30},
		RightThresholdsDBFS: []float64{-58, -58, -44, -34},
	}
	design, err := Build(p, 48000)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got, want := design.GlobalAttenuationDB, 12.0; got != want {
		t.Fatalf("global attenuation = %v, want %v", got, want)
	}
	leftWant := []float64{-12, -9, -2, 0}
	rightWant := []float64{-12, -12, -5, 0}
	for i := range leftWant {
		if math.Abs(design.Left[i].GainDB-leftWant[i]) > 1e-9 {
			t.Fatalf("left[%d] gain = %v, want %v", i, design.Left[i].GainDB, leftWant[i])
		}
		if math.Abs(design.Right[i].GainDB-rightWant[i]) > 1e-9 {
			t.Fatalf("right[%d] gain = %v, want %v", i, design.Right[i].GainDB, rightWant[i])
		}
	}
}

func TestLoudestBandEndsAtExactlyZeroDB(t *testing.T) {
	t.Parallel()
	p := &profile.Profile{
		Version:             profile.Version,
		CreatedAt:           time.Now().UTC(),
		FrequenciesHz:       []float64{80, 200, 500},
		LeftThresholdsDBFS:  []float64{-55, -45, -35},
		RightThresholdsDBFS: []float64{-57, -46, -34},
	}
	design, err := Build(p, 48000)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	maxGain := math.Inf(-1)
	for _, b := range design.Left {
		if b.GainDB > maxGain {
			maxGain = b.GainDB
		}
	}
	for _, b := range design.Right {
		if b.GainDB > maxGain {
			maxGain = b.GainDB
		}
	}
	if maxGain != 0 {
		t.Fatalf("max gain = %v, want 0", maxGain)
	}
}
