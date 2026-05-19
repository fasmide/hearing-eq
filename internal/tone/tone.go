package tone

import (
	"math"
)

const (
	DefaultSampleRate = 48000
	BurstDuration     = 300
	FadeDuration      = 20
)

type Ear int

const (
	Left Ear = iota
	Right
	Both
)

func DBFSToAmplitude(dbfs float64) float64 {
	amp := math.Pow(10, dbfs/20)
	if amp < 0 {
		return 0
	}
	if amp > 1 {
		return 1
	}
	return amp
}

func BurstStereo(freqHz, dbfs float64, ear Ear, sampleRate int) []float32 {
	frames := sampleRate * BurstDuration / 1000
	return BurstStereoFrames(freqHz, dbfs, ear, sampleRate, frames)
}

func BurstStereoFrames(freqHz, dbfs float64, ear Ear, sampleRate, frames int) []float32 {
	out := make([]float32, frames*2)
	amp := DBFSToAmplitude(dbfs)
	fadeFrames := sampleRate * FadeDuration / 1000
	for i := 0; i < frames; i++ {
		env := 1.0
		switch {
		case i < fadeFrames:
			env = 0.5 * (1 - math.Cos(math.Pi*float64(i)/float64(fadeFrames)))
		case i >= frames-fadeFrames:
			n := frames - i - 1
			env = 0.5 * (1 - math.Cos(math.Pi*float64(n)/float64(fadeFrames)))
		}
		sample := float32(math.Sin(2*math.Pi*freqHz*float64(i)/float64(sampleRate)) * amp * env)
		switch ear {
		case Left:
			out[i*2] = sample
		case Right:
			out[i*2+1] = sample
		case Both:
			out[i*2] = sample
			out[i*2+1] = sample
		}
	}
	return out
}

func SilenceStereo(frames int) []float32 {
	return make([]float32, frames*2)
}

func AppendSilenceStereo(samples []float32, frames int) []float32 {
	return append(samples, make([]float32, frames*2)...)
}

func ContinuousStereo(freqHz, dbfs float64, sampleRate, frames int, phase *float64) []float32 {
	out := make([]float32, frames*2)
	amp := DBFSToAmplitude(dbfs)
	for i := 0; i < frames; i++ {
		s := float32(math.Sin(2*math.Pi**phase) * amp)
		out[i*2] = s
		out[i*2+1] = s
		*phase += freqHz / float64(sampleRate)
		if *phase >= 1 {
			*phase -= math.Floor(*phase)
		}
	}
	return out
}
