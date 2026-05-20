package pastream

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jfreymuth/pulse"
)

type ClientWrapper struct {
	client *pulse.Client
}

const (
	SampleRate   = 48000
	Channels     = 2
	BlockFrames  = 256
	blockSamples = BlockFrames * Channels
	blockBytes   = blockSamples * 4
)

func NewClient(appName string) (*pulse.Client, error) {
	client, err := pulse.NewClient(pulse.ClientApplicationName(appName))
	if err != nil {
		return nil, fmt.Errorf("connect pulse audio client: %w", err)
	}
	return client, nil
}

func NewWrappedClient(appName string) (*ClientWrapper, error) {
	client, err := NewClient(appName)
	if err != nil {
		return nil, err
	}
	return &ClientWrapper{client: client}, nil
}

func (c *ClientWrapper) Client() *pulse.Client {
	return c.client
}

func (c *ClientWrapper) Close() {
	if c != nil && c.client != nil {
		c.client.Close()
	}
}

func ResolveHardwareDefaultSink(client *pulse.Client, avoidID string) (*pulse.Sink, error) {
	defaultSink, err := client.DefaultSink()
	if err != nil {
		return nil, fmt.Errorf("resolve default sink: %w", err)
	}
	if avoidID == "" || defaultSink.ID() != avoidID {
		return defaultSink, nil
	}
	sinks, err := client.ListSinks()
	if err != nil {
		return nil, fmt.Errorf("list sinks: %w", err)
	}
	for _, sink := range sinks {
		if sink.ID() != avoidID {
			return sink, nil
		}
	}
	return nil, fmt.Errorf("no playback sink available besides %q", avoidID)
}

func PlayBuffer(client *pulse.Client, sink *pulse.Sink, samples []float32, mediaName string) error {
	reader := &sliceReader{samples: samples}
	options := []pulse.PlaybackOption{
		pulse.PlaybackStereo,
		pulse.PlaybackSampleRate(SampleRate),
		pulse.PlaybackBufferSize(blockSamples),
		pulse.PlaybackMediaName(mediaName),
	}
	if sink != nil {
		options = append(options, pulse.PlaybackSink(sink))
	}
	stream, err := client.NewPlayback(pulse.Float32Reader(reader.Read), options...)
	if err != nil {
		return fmt.Errorf("create playback stream: %w", err)
	}
	defer stream.Close()
	stream.Start()
	stream.Drain()
	if err := stream.Error(); err != nil && !errors.Is(err, pulse.EndOfData) {
		return fmt.Errorf("playback stream error: %w", err)
	}
	return nil
}

type ToneLoop struct {
	stream *pulse.PlaybackStream
	mu     sync.Mutex
	stop   bool
	phase  float64
	freq   float64
	dbfs   float64
	closed bool
}

func StartToneLoop(client *pulse.Client, sink *pulse.Sink, freqHz, dbfs float64, chunk func(freqHz, dbfs float64, sampleRate, frames int, phase *float64) []float32) (*ToneLoop, error) {
	loop := &ToneLoop{freq: freqHz, dbfs: dbfs}
	reader := pulse.Float32Reader(func(out []float32) (int, error) {
		loop.mu.Lock()
		defer loop.mu.Unlock()
		if loop.stop {
			return 0, pulse.EndOfData
		}
		buf := chunk(loop.freq, loop.dbfs, SampleRate, len(out)/Channels, &loop.phase)
		copy(out, buf)
		return len(out), nil
	})
	options := []pulse.PlaybackOption{
		pulse.PlaybackStereo,
		pulse.PlaybackSampleRate(SampleRate),
		pulse.PlaybackBufferSize(blockSamples),
		pulse.PlaybackMediaName("Hearing EQ Calibration"),
	}
	if sink != nil {
		options = append(options, pulse.PlaybackSink(sink))
	}
	stream, err := client.NewPlayback(reader, options...)
	if err != nil {
		return nil, fmt.Errorf("create tone loop stream: %w", err)
	}
	loop.stream = stream
	stream.Start()
	return loop, nil
}

func (l *ToneLoop) Stop() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.stop = true
	l.mu.Unlock()
	l.stream.Drain()
	if err := l.stream.Error(); err != nil && !errors.Is(err, pulse.EndOfData) {
		l.stream.Close()
		return fmt.Errorf("tone loop stream error: %w", err)
	}
	l.stream.Close()
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	return nil
}

type Duplex struct {
	record   *pulse.RecordStream
	playback *pulse.PlaybackStream
	queue    chan []float32
	fatalCh  chan error
}

type ProcessTap func(input []float32, output []float32)

func StartDuplex(client *pulse.Client, monitorSinkID string, process func([]float32), mediaName string) (*Duplex, error) {
	return StartDuplexWithTap(client, monitorSinkID, func(input []float32, output []float32) {
		copy(output, input)
		process(output)
	}, mediaName)
}

func StartDuplexWithTap(client *pulse.Client, monitorSinkID string, process ProcessTap, mediaName string) (*Duplex, error) {
	sink, err := waitForSink(client, monitorSinkID, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defaultSink, err := ResolveHardwareDefaultSink(client, monitorSinkID)
	if err != nil {
		return nil, err
	}
	d := &Duplex{
		queue:   make(chan []float32, 8),
		fatalCh: make(chan error, 1),
	}
		record, err := client.NewRecord(
			pulse.Float32Writer(func(in []float32) (n int, err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("audio record callback panic: %v", r)
					d.reportFatal(err)
				}
			}()
			input := append(make([]float32, 0, len(in)), in...)
			output := append(make([]float32, 0, len(in)), in...)
			process(input, output)
			d.enqueue(output)
			return len(in), nil
		}),
		pulse.RecordStereo,
		pulse.RecordSampleRate(SampleRate),
		pulse.RecordBufferFragmentSize(blockBytes),
		pulse.RecordMonitor(sink),
		pulse.RecordMediaName(mediaName+" Capture"),
	)
	if err != nil {
		return nil, fmt.Errorf("create record stream: %w", err)
	}
	d.record = record
	var pending []float32
	playback, err := client.NewPlayback(
		pulse.Float32Reader(func(out []float32) (n int, err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("audio playback callback panic: %v", r)
					d.reportFatal(err)
				}
			}()
			for i := range out {
				out[i] = 0
			}
			written := 0
			for written < len(out) {
				if len(pending) == 0 {
					select {
					case pending = <-d.queue:
					default:
						return len(out), nil
					}
				}
				copied := copy(out[written:], pending)
				written += copied
				pending = pending[copied:]
			}
			return len(out), nil
		}),
		pulse.PlaybackStereo,
		pulse.PlaybackSampleRate(SampleRate),
		pulse.PlaybackBufferSize(blockSamples),
		pulse.PlaybackSink(defaultSink),
		pulse.PlaybackMediaName(mediaName+" Output"),
	)
	if err != nil {
		record.Close()
		return nil, fmt.Errorf("create playback stream: %w", err)
	}
	d.playback = playback
	d.record.Start()
	d.playback.Start()
	return d, nil
}

func (d *Duplex) Errors() <-chan error {
	return d.fatalCh
}

func (d *Duplex) Close() error {
	if d == nil {
		return nil
	}
	if d.record != nil {
		d.record.Stop()
		d.record.Close()
	}
	if d.playback != nil {
		d.playback.Stop()
		d.playback.Close()
	}
	return nil
}

func (d *Duplex) enqueue(buf []float32) {
	select {
	case d.queue <- buf:
		return
	default:
	}
	select {
	case <-d.queue:
	default:
	}
	select {
	case d.queue <- buf:
	default:
	}
}

func (d *Duplex) reportFatal(err error) {
	select {
	case d.fatalCh <- err:
	default:
	}
}

type sliceReader struct {
	samples []float32
	offset  int
}

func (r *sliceReader) Read(out []float32) (int, error) {
	if r.offset >= len(r.samples) {
		return 0, pulse.EndOfData
	}
	n := copy(out, r.samples[r.offset:])
	r.offset += n
	if r.offset >= len(r.samples) {
		return n, pulse.EndOfData
	}
	return n, nil
}

func waitForSink(client *pulse.Client, id string, timeout time.Duration) (*pulse.Sink, error) {
	deadline := time.Now().Add(timeout)
	for {
		sink, err := client.SinkByID(id)
		if err == nil {
			return sink, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("resolve sink %q: %w", id, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
