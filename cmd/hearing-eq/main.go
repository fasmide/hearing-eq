package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"gioui.org/app"
	"github.com/jfreymuth/pulse"

	"hearing-eq/internal/engine"
	"hearing-eq/internal/pastream"
	"hearing-eq/internal/tone"
	ui "hearing-eq/ui/hearprofile"
)

func main() {
	log.SetFlags(log.LstdFlags)
	headless := flag.Bool("headless", false, "run EQ engine without the GUI")
	flag.Parse()

	eng, err := engine.New()
	if err != nil {
		log.Printf("hearing-eq: %v", err)
		os.Exit(1)
	}
	defer eng.Close()

	if *headless {
		if err := eng.RunUntilSignal(); err != nil {
			log.Printf("hearing-eq: %v", err)
			os.Exit(1)
		}
		return
	}

	go func() {
		w := new(app.Window)
		w.Option(app.Title("hearing-eq"), app.Size(1100, 820))
		audio, err := newPlayer()
		if err != nil {
			fmt.Fprintf(os.Stderr, "hearing-eq: %v\n", err)
			os.Exit(1)
		}
		defer audio.Close()
		if _, err := eng.StartIfProfileExists(); err != nil {
			fmt.Fprintf(os.Stderr, "hearing-eq: %v\n", err)
			os.Exit(1)
		}
		if err := ui.New(w, audio, eng).Run(); err != nil {
			fmt.Fprintf(os.Stderr, "hearing-eq: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}

func newPlayer() (*pulsePlayer, error) {
	client, err := pastream.NewClient("hearing-eq-profile")
	if err != nil {
		return nil, err
	}
	sink, err := pastream.ResolveHardwareDefaultSink(client, "hearing-eq")
	if err != nil {
		client.Close()
		return nil, err
	}
	return &pulsePlayer{client: client, sink: sink}, nil
}

type pulsePlayer struct {
	client *pulse.Client
	sink   *pulse.Sink
	loop   *pastream.ToneLoop
}

func (p *pulsePlayer) PlayBuffer(samples []float32, mediaName string) error {
	return pastream.PlayBuffer(p.client, p.sink, samples, mediaName)
}

func (p *pulsePlayer) StartCalibrationTone(freqHz, dbfs float64) error {
	if p.loop != nil {
		if err := p.loop.Stop(); err != nil {
			return err
		}
	}
	loop, err := pastream.StartToneLoop(p.client, p.sink, freqHz, dbfs, tone.ContinuousStereo)
	if err != nil {
		return err
	}
	p.loop = loop
	return nil
}

func (p *pulsePlayer) StopCalibrationTone() error {
	if p.loop == nil {
		return nil
	}
	err := p.loop.Stop()
	p.loop = nil
	return err
}

func (p *pulsePlayer) Close() error {
	_ = p.StopCalibrationTone()
	if p.client != nil {
		p.client.Close()
	}
	return nil
}
