package main

import (
	"fmt"
	"os"

	"gioui.org/app"
	"github.com/jfreymuth/pulse"

	"hearing-eq/internal/pastream"
	"hearing-eq/internal/tone"
	ui "hearing-eq/ui/hearprofile"
)

func main() {
	go func() {
		w := new(app.Window)
		w.Option(app.Title("hearprofile"), app.Size(900, 700))
		audio, err := newPlayer()
		if err != nil {
			fmt.Fprintf(os.Stderr, "hearprofile: %v\n", err)
			os.Exit(1)
		}
		defer audio.Close()
		if err := ui.New(w, audio).Run(); err != nil {
			fmt.Fprintf(os.Stderr, "hearprofile: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}

func newPlayer() (*pulsePlayer, error) {
	client, err := pastream.NewClient("hearprofile")
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
