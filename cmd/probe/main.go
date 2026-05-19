package main

import (
	"fmt"
	"os"

	"hearing-eq/internal/pastream"
	"hearing-eq/internal/tone"
)

func main() {
	client, err := pastream.NewClient("hearing-eq-probe")
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	samples := tone.BurstStereoFrames(1000, -20, tone.Both, tone.DefaultSampleRate, 2*tone.DefaultSampleRate)
	if err := pastream.PlayBuffer(client, nil, samples, "Hearing EQ Probe"); err != nil {
		fmt.Fprintf(os.Stderr, "probe: %v\n", err)
		os.Exit(1)
	}
}
