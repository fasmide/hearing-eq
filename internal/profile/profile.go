package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
)

const (
	Version     = 1
	DirName     = "hearing-eq"
	FileName    = "profile.json"
	tmpFileName = "profile.json.tmp"
)

var DefaultFrequenciesHz = []float64{80, 200, 500, 1000, 2000, 3000, 4500, 7000, 10000}

type Profile struct {
	Version            int       `json:"version"`
	CreatedAt          time.Time `json:"created_at"`
	FrequenciesHz      []float64 `json:"frequencies_hz"`
	LeftThresholdsDBFS []float64 `json:"left_thresholds_dbfs"`
	RightThresholdsDBFS []float64 `json:"right_thresholds_dbfs"`
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", DirName, FileName), nil
}

func LoadDefault() (*Profile, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return Load(path)
}

func SaveDefault(p *Profile) error {
	path, err := DefaultPath()
	if err != nil {
		return err
	}
	return Save(path, p)
}

func Load(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read profile %q: %w", path, err)
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("decode profile %q: %w", path, err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("validate profile %q: %w", path, err)
	}
	return &p, nil
}

func Save(path string, p *Profile) (err error) {
	if p == nil {
		return errors.New("profile is nil")
	}
	if err := p.Validate(); err != nil {
		return fmt.Errorf("validate profile: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create profile directory %q: %w", dir, err)
	}
	tmpPath := filepath.Join(dir, tmpFileName)
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create temp profile %q: %w", tmpPath, err)
	}
	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	if err := enc.Encode(p); err != nil {
		_ = file.Close()
		return fmt.Errorf("encode profile to %q: %w", tmpPath, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync temp profile %q: %w", tmpPath, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temp profile %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace profile %q: %w", path, err)
	}
	dirFile, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open profile directory %q: %w", dir, err)
	}
	defer dirFile.Close()
	if err := dirFile.Sync(); err != nil {
		return fmt.Errorf("sync profile directory %q: %w", dir, err)
	}
	if err := os.Remove(tmpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale temp profile %q: %w", tmpPath, err)
	}
	return nil
}

func (p *Profile) Validate() error {
	if p == nil {
		return errors.New("profile is nil")
	}
	if p.Version != Version {
		return fmt.Errorf("unsupported version %d", p.Version)
	}
	if p.CreatedAt.IsZero() {
		return errors.New("created_at must be set")
	}
	if len(p.FrequenciesHz) == 0 {
		return errors.New("frequencies_hz must not be empty")
	}
	if len(p.LeftThresholdsDBFS) != len(p.FrequenciesHz) {
		return errors.New("left_thresholds_dbfs length must match frequencies_hz")
	}
	if len(p.RightThresholdsDBFS) != len(p.FrequenciesHz) {
		return errors.New("right_thresholds_dbfs length must match frequencies_hz")
	}
	for i, f := range p.FrequenciesHz {
		if !isFinite(f) || f <= 0 {
			return fmt.Errorf("frequencies_hz[%d] must be positive finite", i)
		}
	}
	for i, v := range p.LeftThresholdsDBFS {
		if !isFinite(v) || v > 0 {
			return fmt.Errorf("left_thresholds_dbfs[%d] must be finite and <= 0", i)
		}
	}
	for i, v := range p.RightThresholdsDBFS {
		if !isFinite(v) || v > 0 {
			return fmt.Errorf("right_thresholds_dbfs[%d] must be finite and <= 0", i)
		}
	}
	return nil
}

func New(left, right []float64) (*Profile, error) {
	p := &Profile{
		Version:             Version,
		CreatedAt:           time.Now().UTC(),
		FrequenciesHz:       append([]float64(nil), DefaultFrequenciesHz...),
		LeftThresholdsDBFS:  append([]float64(nil), left...),
		RightThresholdsDBFS: append([]float64(nil), right...),
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("build profile: %w", err)
	}
	return p, nil
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
