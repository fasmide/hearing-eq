package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"hearing-eq/internal/biquad"
	"hearing-eq/internal/eqdesign"
	"hearing-eq/internal/pasink"
	"hearing-eq/internal/pastream"
	"hearing-eq/internal/profile"

	control "github.com/the-jonsey/pulseaudio"
)

type coefficientSet struct {
	left  []biquad.Coefficients
	right []biquad.Coefficients
}

func main() {
	log.SetFlags(log.LstdFlags)
	if err := run(); err != nil {
		log.Printf("heareq: %v", err)
		os.Exit(1)
	}
}

func run() error {
	prof, coeffs, err := loadCoefficients()
	if err != nil {
		return err
	}
	log.Printf("loaded profile created %s with %d bands", prof.CreatedAt.Format("2006-01-02 15:04:05Z07:00"), len(coeffs.left))

	controlClient, err := control.NewClient()
	if err != nil {
		return fmt.Errorf("connect pulseaudio control client: %w", err)
	}
	defer controlClient.Close()

	virtualSink, err := pasink.Create(controlClient)
	if err != nil {
		return err
	}
	defer func() {
		if err := virtualSink.Close(); err != nil {
			log.Printf("heareq teardown: %v", err)
		}
	}()
	log.Printf("created virtual sink %q with module index %d", virtualSink.Name(), virtualSink.ModuleIndex())

	audioClient, err := pastream.NewClient("heareq")
	if err != nil {
		return err
	}
	defer audioClient.Close()

	var active atomic.Pointer[coefficientSet]
	active.Store(coeffs)
	duplex, err := pastream.StartDuplex(audioClient, virtualSink.Name(), processor(&active), "Hearing EQ")
	if err != nil {
		return err
	}
	defer duplex.Close()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	for {
		select {
		case err := <-duplex.Errors():
			return fmt.Errorf("audio loop failed: %w", err)
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGHUP:
				_, next, err := loadCoefficients()
				if err != nil {
					log.Printf("reload profile failed: %v", err)
					continue
				}
				active.Store(next)
				log.Printf("reloaded profile coefficients")
			case syscall.SIGINT, syscall.SIGTERM:
				log.Printf("shutting down on %s", sig)
				return nil
			}
		}
	}
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

func processor(active *atomic.Pointer[coefficientSet]) func([]float32) {
	var leftChain biquad.Chain
	var rightChain biquad.Chain
	var current *coefficientSet
	return func(samples []float32) {
		coeffs := active.Load()
		if coeffs != current {
			leftChain = biquad.NewChain(coeffs.left)
			rightChain = biquad.NewChain(coeffs.right)
			current = coeffs
		}
		for i := 0; i+1 < len(samples); i += 2 {
			l := leftChain.ProcessSample(float64(samples[i]))
			r := rightChain.ProcessSample(float64(samples[i+1]))
			samples[i] = float32(l)
			samples[i+1] = float32(r)
		}
	}
}
