package engine

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"hearing-eq/internal/biquad"
	"hearing-eq/internal/eqdesign"
	"hearing-eq/internal/pasink"
	"hearing-eq/internal/pastream"
	"hearing-eq/internal/profile"
	"hearing-eq/internal/spectrum"

	control "github.com/the-jonsey/pulseaudio"
)

type coefficientSet struct {
	left  []biquad.Coefficients
	right []biquad.Coefficients
}

type Engine struct {
	controlClient *control.Client
	audioClient   *pastream.ClientWrapper
	virtualSink   *pasink.Sink
	duplex        *pastream.Duplex
	spectrum      *spectrum.Analyzer
	active        atomic.Pointer[coefficientSet]
	mu            sync.Mutex
	statsMu       sync.Mutex
	running       bool
	lastProcess   time.Time
	bufferRate    float64
	frameRate     float64
}

type AudioStats struct {
	BuffersPerSecond float64
	FramesPerSecond  float64
}

func New() (*Engine, error) {
	controlClient, err := control.NewClient()
	if err != nil {
		return nil, fmt.Errorf("connect pulseaudio control client: %w", err)
	}
	audioClient, err := pastream.NewWrappedClient("hearing-eq")
	if err != nil {
		controlClient.Close()
		return nil, err
	}
	analyzer, err := spectrum.New()
	if err != nil {
		audioClient.Close()
		controlClient.Close()
		return nil, fmt.Errorf("create spectrum analyzer: %w", err)
	}
	return &Engine{controlClient: controlClient, audioClient: audioClient, spectrum: analyzer}, nil
}

func (e *Engine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		return nil
	}
	_, coeffs, err := loadCoefficients()
	if err != nil {
		return err
	}
	e.active.Store(coeffs)
	virtualSink, err := pasink.Create(e.controlClient)
	if err != nil {
		return err
	}
	e.virtualSink = virtualSink
	duplex, err := pastream.StartDuplexWithTap(e.audioClient.Client(), virtualSink.Name(), e.processor(), "Hearing EQ")
	if err != nil {
		_ = virtualSink.Close()
		e.virtualSink = nil
		return err
	}
	e.duplex = duplex
	e.running = true
	log.Printf("created virtual sink %q with module index %d", virtualSink.Name(), virtualSink.ModuleIndex())
	return nil
}

func (e *Engine) StartIfProfileExists() (bool, error) {
	err := e.Start()
	if err == nil {
		return true, nil
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return false, nil
	}
	return false, err
}

func (e *Engine) RunUntilSignal() error {
	if err := e.Start(); err != nil {
		return err
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	for {
		select {
		case err := <-e.duplex.Errors():
			return fmt.Errorf("audio loop failed: %w", err)
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGHUP:
				if err := e.ReloadProfile(); err != nil {
					log.Printf("reload profile failed: %v", err)
				} else {
					log.Printf("reloaded profile coefficients")
				}
			case syscall.SIGINT, syscall.SIGTERM:
				log.Printf("shutting down on %s", sig)
				return nil
			}
		}
	}
}

func (e *Engine) ReloadProfile() error {
	_, coeffs, err := loadCoefficients()
	if err != nil {
		return err
	}
	e.active.Store(coeffs)
	return nil
}

func (e *Engine) EnsureStartedWithCurrentProfile() error {
	e.mu.Lock()
	running := e.running
	e.mu.Unlock()
	if running {
		return e.ReloadProfile()
	}
	_, err := e.StartIfProfileExists()
	return err
}

func (e *Engine) Spectrum() spectrum.Snapshot {
	return e.spectrum.Snapshot()
}

func (e *Engine) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

func (e *Engine) AudioStats() AudioStats {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	return AudioStats{
		BuffersPerSecond: e.bufferRate,
		FramesPerSecond:  e.frameRate,
	}
}

func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.duplex != nil {
		_ = e.duplex.Close()
		e.duplex = nil
	}
	if e.virtualSink != nil {
		if err := e.virtualSink.Close(); err != nil {
			return err
		}
		e.virtualSink = nil
	}
	if e.audioClient != nil {
		e.audioClient.Close()
	}
	if e.controlClient != nil {
		e.controlClient.Close()
	}
	e.running = false
	return nil
}

func (e *Engine) processor() func(input []float32, output []float32) {
	var leftChain biquad.Chain
	var rightChain biquad.Chain
	var current *coefficientSet
	return func(input []float32, output []float32) {
		e.noteProcess(len(input) / 2)
		coeffs := e.active.Load()
		if coeffs != current {
			leftChain = biquad.NewChain(coeffs.left)
			rightChain = biquad.NewChain(coeffs.right)
			current = coeffs
		}
		copy(output, input)
		for i := 0; i+1 < len(output); i += 2 {
			l := leftChain.ProcessSample(float64(output[i]))
			r := rightChain.ProcessSample(float64(output[i+1]))
			output[i] = float32(l)
			output[i+1] = float32(r)
		}
		e.spectrum.Push(input, output)
	}
}

func (e *Engine) noteProcess(frames int) {
	now := time.Now()
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	if !e.lastProcess.IsZero() {
		dt := now.Sub(e.lastProcess).Seconds()
		if dt > 0 {
			instantBuffers := 1 / dt
			instantFrames := float64(frames) / dt
			if e.bufferRate == 0 {
				e.bufferRate = instantBuffers
				e.frameRate = instantFrames
			} else {
				e.bufferRate = e.bufferRate*0.85 + instantBuffers*0.15
				e.frameRate = e.frameRate*0.85 + instantFrames*0.15
			}
		}
	}
	e.lastProcess = now
}

func loadCoefficients() (*profile.Profile, *coefficientSet, error) {
	prof, err := profile.LoadDefault()
	if err != nil {
		return nil, nil, err
	}
	design, err := eqdesign.Build(prof, pastream.SampleRate)
	if err != nil {
		return nil, nil, fmt.Errorf("build EQ design: %w", err)
	}
	left := make([]biquad.Coefficients, len(design.Left))
	right := make([]biquad.Coefficients, len(design.Right))
	for i := range design.Left {
		left[i] = design.Left[i].Coefficients
		right[i] = design.Right[i].Coefficients
	}
	return prof, &coefficientSet{left: left, right: right}, nil
}
