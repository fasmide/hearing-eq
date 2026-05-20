package spectrum

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/10d9e/gofft"
)

const (
	DefaultFFTSize = 1024
	HopSize        = 256
	maxDisplayHz   = 10000
	attackAlpha    = 0.85
	releaseAlpha   = 0.45
)

type Snapshot struct {
	UpdatedAt time.Time
	UpdatesPerSecond float64
	FrequenciesHz []float64
	InputDB   []float64
	LeftDB    []float64
	RightDB   []float64
}

type Analyzer struct {
	mu        sync.RWMutex
	fft       gofft.Fft
	fftSize   int
	window    []float64
	inputBuf  []float64
	leftBuf   []float64
	rightBuf  []float64
	tmpInput  []float64
	tmpLeft   []float64
	tmpRight  []float64
	writePos  int
	filled    int
	pending   int
	lastUpdate time.Time
	ups       float64
	snapshot  Snapshot
}

func New() (*Analyzer, error) {
	planner := gofft.NewPlanner()
	fft := planner.PlanForward(DefaultFFTSize)
	if fft == nil {
		return nil, fmt.Errorf("create FFT planner")
	}
	window := make([]float64, DefaultFFTSize)
	for i := range window {
		window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(DefaultFFTSize-1))
	}
	return &Analyzer{
		fft:      fft,
		fftSize:  DefaultFFTSize,
		window:   window,
		inputBuf: make([]float64, DefaultFFTSize),
		leftBuf:  make([]float64, DefaultFFTSize),
		rightBuf: make([]float64, DefaultFFTSize),
		tmpInput: make([]float64, DefaultFFTSize),
		tmpLeft:  make([]float64, DefaultFFTSize),
		tmpRight: make([]float64, DefaultFFTSize),
		snapshot: Snapshot{
			FrequenciesHz: binFrequencies(DefaultFFTSize),
		},
	}, nil
}

func (a *Analyzer) Push(input, output []float32) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := 0; i+1 < len(input) && i+1 < len(output); i += 2 {
		monoIn := 0.5 * (float64(input[i]) + float64(input[i+1]))
		a.inputBuf[a.writePos] = monoIn
		a.leftBuf[a.writePos] = float64(output[i])
		a.rightBuf[a.writePos] = float64(output[i+1])
		a.writePos++
		if a.writePos == a.fftSize {
			a.writePos = 0
		}
		if a.filled < a.fftSize {
			a.filled++
		}
		a.pending++
	}
	if a.filled < a.fftSize || a.pending < HopSize {
		return
	}
	a.pending = 0
	now := time.Now()
	if !a.lastUpdate.IsZero() {
		dt := now.Sub(a.lastUpdate).Seconds()
		if dt > 0 {
			instant := 1 / dt
			if a.ups == 0 {
				a.ups = instant
			} else {
				a.ups = a.ups*0.85 + instant*0.15
			}
		}
	}
	a.lastUpdate = now
	a.snapshot.UpdatedAt = now
	a.snapshot.UpdatesPerSecond = a.ups
	copyFromRing(a.tmpInput, a.inputBuf, a.writePos)
	copyFromRing(a.tmpLeft, a.leftBuf, a.writePos)
	copyFromRing(a.tmpRight, a.rightBuf, a.writePos)
	a.snapshot.InputDB = computeSpectrum(a.fft, a.window, a.snapshot.FrequenciesHz, a.tmpInput, a.snapshot.InputDB)
	a.snapshot.LeftDB = computeSpectrum(a.fft, a.window, a.snapshot.FrequenciesHz, a.tmpLeft, a.snapshot.LeftDB)
	a.snapshot.RightDB = computeSpectrum(a.fft, a.window, a.snapshot.FrequenciesHz, a.tmpRight, a.snapshot.RightDB)
}

func (a *Analyzer) Snapshot() Snapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return Snapshot{
		UpdatedAt: a.snapshot.UpdatedAt,
		UpdatesPerSecond: a.snapshot.UpdatesPerSecond,
		FrequenciesHz: append([]float64(nil), a.snapshot.FrequenciesHz...),
		InputDB:   append([]float64(nil), a.snapshot.InputDB...),
		LeftDB:    append([]float64(nil), a.snapshot.LeftDB...),
		RightDB:   append([]float64(nil), a.snapshot.RightDB...),
	}
}


func copyFromRing(dst, src []float64, writePos int) {
	n := copy(dst, src[writePos:])
	copy(dst[n:], src[:writePos])
}

func computeSpectrum(fft gofft.Fft, window, frequenciesHz, samples, reuse []float64) []float64 {
	buf := make([]complex128, len(samples))
	for i := range samples {
		buf[i] = complex(samples[i]*window[i], 0)
	}
	fft.Process(buf)
	if reuse == nil || len(reuse) != len(frequenciesHz) {
		reuse = make([]float64, len(frequenciesHz))
	}
	for i := range frequenciesHz {
		bin := i + 1
		mag := cmplxAbs(buf[bin]) / float64(len(buf))
		current := 20 * math.Log10(math.Max(mag, 1e-12))
		if reuse[i] == 0 {
			reuse[i] = current
		} else {
			alpha := releaseAlpha
			if current > reuse[i] {
				alpha = attackAlpha
			}
			reuse[i] = reuse[i]*(1-alpha) + current*alpha
		}
	}
	return reuse
}

func cmplxAbs(v complex128) float64 {
	return math.Hypot(real(v), imag(v))
}


func binFrequencies(fftSize int) []float64 {
	half := fftSize / 2
	frequencies := make([]float64, 0, half)
	for bin := 1; bin < half; bin++ {
		freq := float64(bin) * 48000 / float64(fftSize)
		if freq > maxDisplayHz {
			break
		}
		frequencies = append(frequencies, freq)
	}
	return frequencies
}
