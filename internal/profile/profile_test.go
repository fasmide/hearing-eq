package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	want := &Profile{
		Version:              Version,
		CreatedAt:            time.Date(2026, 5, 18, 14, 32, 0, 0, time.UTC),
		FrequenciesHz:        append([]float64(nil), DefaultFrequenciesHz...),
		LeftThresholdsDBFS:   []float64{-42.5, -48.0, -52.5, -55.0, -53.5, -50.0, -45.0, -38.5, -32.0},
		RightThresholdsDBFS:  []float64{-41.0, -47.5, -53.0, -55.5, -54.0, -49.5, -44.0, -37.0, -30.5},
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Version != want.Version || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("round-trip metadata mismatch: got %#v want %#v", got, want)
	}
	if len(got.FrequenciesHz) != len(want.FrequenciesHz) {
		t.Fatalf("frequency length mismatch: got %d want %d", len(got.FrequenciesHz), len(want.FrequenciesHz))
	}
	for i := range want.FrequenciesHz {
		if got.FrequenciesHz[i] != want.FrequenciesHz[i] || got.LeftThresholdsDBFS[i] != want.LeftThresholdsDBFS[i] || got.RightThresholdsDBFS[i] != want.RightThresholdsDBFS[i] {
			t.Fatalf("round-trip mismatch at index %d: got %#v want %#v", i, got, want)
		}
	}
}

func TestLoadRejectsMalformedAndIncomplete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	malformed := filepath.Join(dir, "malformed.json")
	if err := os.WriteFile(malformed, []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Load(malformed); err == nil || !strings.Contains(err.Error(), "decode profile") {
		t.Fatalf("Load() malformed error = %v, want decode failure", err)
	}
	incomplete := filepath.Join(dir, "incomplete.json")
	if err := os.WriteFile(incomplete, []byte(`{"version":1,"created_at":"2026-05-18T14:32:00Z","frequencies_hz":[80]}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Load(incomplete); err == nil || !strings.Contains(err.Error(), "validate profile") {
		t.Fatalf("Load() incomplete error = %v, want validation failure", err)
	}
}

func TestAtomicReplaceLeavesNoTmpFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	p := &Profile{
		Version:             Version,
		CreatedAt:           time.Now().UTC(),
		FrequenciesHz:       append([]float64(nil), DefaultFrequenciesHz...),
		LeftThresholdsDBFS:  []float64{-40, -40, -40, -40, -40, -40, -40, -40, -40},
		RightThresholdsDBFS: []float64{-41, -41, -41, -41, -41, -41, -41, -41, -41},
	}
	if err := Save(path, p); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	if err := Save(path, p); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, tmpFileName)); !os.IsNotExist(err) {
		t.Fatalf("temp file state = %v, want not exists", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("final profile missing: %v", err)
	}
}
