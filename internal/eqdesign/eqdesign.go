package eqdesign

import (
	"fmt"
	"math"

	"hearing-eq/internal/biquad"
	"hearing-eq/internal/profile"
)

const (
	DefaultQ      = 1.4
	MaxRawBoostDB = 12.0
)

type Band struct {
	FrequencyHz float64
	GainDB      float64
	Q           float64
	Coefficients biquad.Coefficients
}

type Design struct {
	Left               []Band
	Right              []Band
	GlobalAttenuationDB float64
}

func Build(p *profile.Profile, sampleRate float64) (*Design, error) {
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("validate profile: %w", err)
	}
	leftRaw := rawBoosts(p.LeftThresholdsDBFS)
	rightRaw := rawBoosts(p.RightThresholdsDBFS)
	global := 0.0
	for _, v := range leftRaw {
		global = math.Max(global, v)
	}
	for _, v := range rightRaw {
		global = math.Max(global, v)
	}
	left := make([]Band, len(p.FrequenciesHz))
	right := make([]Band, len(p.FrequenciesHz))
	for i, freq := range p.FrequenciesHz {
		left[i] = Band{
			FrequencyHz: freq,
			GainDB:      leftRaw[i] - global,
			Q:           DefaultQ,
		}
		left[i].Coefficients = biquad.Peaking(sampleRate, freq, left[i].GainDB, left[i].Q)
		right[i] = Band{
			FrequencyHz: freq,
			GainDB:      rightRaw[i] - global,
			Q:           DefaultQ,
		}
		right[i].Coefficients = biquad.Peaking(sampleRate, freq, right[i].GainDB, right[i].Q)
	}
	return &Design{Left: left, Right: right, GlobalAttenuationDB: global}, nil
}

func rawBoosts(thresholds []float64) []float64 {
	ref := thresholds[0]
	for _, v := range thresholds[1:] {
		if v < ref {
			ref = v
		}
	}
	out := make([]float64, len(thresholds))
	for i, v := range thresholds {
		boost := (v - ref) * 0.5
		if boost > MaxRawBoostDB {
			boost = MaxRawBoostDB
		}
		out[i] = boost
	}
	return out
}
