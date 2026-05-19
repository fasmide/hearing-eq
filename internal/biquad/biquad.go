package biquad

import "math"

type Coefficients struct {
	B0 float64
	B1 float64
	B2 float64
	A1 float64
	A2 float64
}

type State struct {
	Z1 float64
	Z2 float64
}

func Peaking(sampleRate, frequencyHz, gainDB, q float64) Coefficients {
	a := math.Pow(10, gainDB/40)
	w0 := 2 * math.Pi * frequencyHz / sampleRate
	alpha := math.Sin(w0) / (2 * q)
	cosw0 := math.Cos(w0)

	b0 := 1 + alpha*a
	b1 := -2 * cosw0
	b2 := 1 - alpha*a
	a0 := 1 + alpha/a
	a1 := -2 * cosw0
	a2 := 1 - alpha/a

	return Coefficients{
		B0: b0 / a0,
		B1: b1 / a0,
		B2: b2 / a0,
		A1: a1 / a0,
		A2: a2 / a0,
	}
}

func (s *State) ProcessSample(c Coefficients, in float64) float64 {
	out := c.B0*in + s.Z1
	s.Z1 = c.B1*in - c.A1*out + s.Z2
	s.Z2 = c.B2*in - c.A2*out
	return out
}

func (s *State) Reset() {
	s.Z1 = 0
	s.Z2 = 0
}

type Chain struct {
	Coefficients []Coefficients
	States       []State
}

func NewChain(coeffs []Coefficients) Chain {
	return Chain{
		Coefficients: append([]Coefficients(nil), coeffs...),
		States:       make([]State, len(coeffs)),
	}
}

func (c *Chain) Reset() {
	for i := range c.States {
		c.States[i].Reset()
	}
}

func (c *Chain) ProcessSample(in float64) float64 {
	out := in
	for i := range c.Coefficients {
		out = c.States[i].ProcessSample(c.Coefficients[i], out)
	}
	return out
}

func FrequencyResponseMagnitude(c Coefficients, frequencyHz, sampleRate float64) float64 {
	w := 2 * math.Pi * frequencyHz / sampleRate
	cos1 := math.Cos(w)
	sin1 := math.Sin(w)
	cos2 := math.Cos(2 * w)
	sin2 := math.Sin(2 * w)
	nr := c.B0 + c.B1*cos1 + c.B2*cos2
	ni := -c.B1*sin1 - c.B2*sin2
	dr := 1 + c.A1*cos1 + c.A2*cos2
	di := -c.A1*sin1 - c.A2*sin2
	nm := math.Hypot(nr, ni)
	dm := math.Hypot(dr, di)
	if dm == 0 {
		return 0
	}
	return nm / dm
}
