package biquad

import (
	"math"
	"testing"
)

func TestPeakingCoefficientsMatchReference(t *testing.T) {
	t.Parallel()
	got := Peaking(48000, 1000, 6, 1)
	want := Coefficients{
		B0: 1.04395308699,
		B1: -1.89532072394,
		B2: 0.86772228476,
		A1: -1.89532072394,
		A2: 0.91167537175,
	}
	const tol = 1e-6
	if math.Abs(got.B0-want.B0) > tol || math.Abs(got.B1-want.B1) > tol || math.Abs(got.B2-want.B2) > tol || math.Abs(got.A1-want.A1) > tol || math.Abs(got.A2-want.A2) > tol {
		t.Fatalf("coefficients = %#v, want %#v", got, want)
	}
}

func TestImpulseCenterFrequencyGainWithinPointOneDB(t *testing.T) {
	t.Parallel()
	coeffs := Peaking(48000, 1000, 6, 1)
	state := State{}
	impulse := make([]float64, 4096)
	impulse[0] = 1
	response := make([]float64, len(impulse))
	for i := range impulse {
		response[i] = state.ProcessSample(coeffs, impulse[i])
	}
	var realPart, imagPart float64
	w := 2 * math.Pi * 1000 / 48000
	for n, x := range response {
		angle := -w * float64(n)
		realPart += x * math.Cos(angle)
		imagPart += x * math.Sin(angle)
	}
	mag := math.Hypot(realPart, imagPart)
	gainDB := 20 * math.Log10(mag)
	if diff := math.Abs(gainDB - 6); diff > 0.1 {
		t.Fatalf("center gain = %.4f dB, want 6 dB within 0.1 dB", gainDB)
	}
	respMag := FrequencyResponseMagnitude(coeffs, 1000, 48000)
	respDB := 20 * math.Log10(respMag)
	if diff := math.Abs(respDB - 6); diff > 0.1 {
		t.Fatalf("analytical response = %.4f dB, want 6 dB within 0.1 dB", respDB)
	}
}
