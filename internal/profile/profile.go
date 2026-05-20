package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	Version     = 1
	DirName     = "hearing-eq"
	FileName    = "profile.json"
	tmpFileName = "profile.json.tmp"
	ProfilesDirName = "profiles"
	CurrentNameFile = "current_profile.txt"
	FlatProfileName = "Flat"
)

var DefaultFrequenciesHz = []float64{80, 200, 500, 1000, 2000, 3000, 4500, 7000, 10000}

type Profile struct {
	Version            int       `json:"version"`
	CreatedAt          time.Time `json:"created_at"`
	FrequenciesHz      []float64 `json:"frequencies_hz"`
	LeftThresholdsDBFS []float64 `json:"left_thresholds_dbfs"`
	RightThresholdsDBFS []float64 `json:"right_thresholds_dbfs"`
}

type NamedProfile struct {
	Name    string
	Path    string
	Profile *Profile
}

func DefaultPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

func ProfilesDir() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ProfilesDirName), nil
}

func CurrentNamePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, CurrentNameFile), nil
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", DirName), nil
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

func SaveNamed(name string, p *Profile) error {
	name, err := sanitizeName(name)
	if err != nil {
		return err
	}
	if name == FlatProfileName {
		return fmt.Errorf("%q is reserved", FlatProfileName)
	}
	dir, err := ProfilesDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name+".json")
	if err := Save(path, p); err != nil {
		return err
	}
	if err := SetCurrentName(name); err != nil {
		return err
	}
	return SaveDefault(p)
}

func LoadCurrent() (*NamedProfile, error) {
	name, err := CurrentName()
	if err == nil && name != "" {
		p, err := LoadNamed(name)
		if err == nil {
			return p, nil
		}
	}
	p, err := LoadDefault()
	if err != nil {
		return nil, err
	}
	return &NamedProfile{Profile: p}, nil
}

func LoadNamed(name string) (*NamedProfile, error) {
	name, err := sanitizeName(name)
	if err != nil {
		return nil, err
	}
	if name == FlatProfileName {
		p := FlatProfile()
		return &NamedProfile{Name: FlatProfileName, Profile: p}, nil
	}
	dir, err := ProfilesDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name+".json")
	p, err := Load(path)
	if err != nil {
		return nil, err
	}
	return &NamedProfile{Name: name, Path: path, Profile: p}, nil
}

func List() ([]NamedProfile, error) {
	dir, err := ProfilesDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create profiles directory %q: %w", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read profiles directory %q: %w", dir, err)
	}
	out := make([]NamedProfile, 0, len(entries))
	out = append(out, NamedProfile{Name: FlatProfileName, Profile: FlatProfile()})
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		p, err := Load(path)
		if err != nil {
			continue
		}
		out = append(out, NamedProfile{
			Name:    entry.Name()[:len(entry.Name())-len(filepath.Ext(entry.Name()))],
			Path:    path,
			Profile: p,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func CurrentName() (string, error) {
	path, err := CurrentNamePath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read current profile name %q: %w", path, err)
	}
	return sanitizeName(string(bytesTrimSpace(data)))
}

func SetCurrentName(name string) error {
	name, err := sanitizeName(name)
	if err != nil {
		return err
	}
	path, err := CurrentNamePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create profile config directory: %w", err)
	}
	if err := os.WriteFile(path, append([]byte(name), '\n'), 0o644); err != nil {
		return fmt.Errorf("write current profile name %q: %w", path, err)
	}
	return nil
}

func Rename(oldName, newName string) error {
	oldName, err := sanitizeName(oldName)
	if err != nil {
		return err
	}
	if oldName == FlatProfileName {
		return fmt.Errorf("%q cannot be renamed", FlatProfileName)
	}
	newName, err = sanitizeName(newName)
	if err != nil {
		return err
	}
	if newName == FlatProfileName {
		return fmt.Errorf("%q is reserved", FlatProfileName)
	}
	if oldName == newName {
		return nil
	}
	dir, err := ProfilesDir()
	if err != nil {
		return err
	}
	oldPath := filepath.Join(dir, oldName+".json")
	newPath := filepath.Join(dir, newName+".json")
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("profile %q already exists", newName)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check target profile %q: %w", newName, err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("rename profile %q to %q: %w", oldName, newName, err)
	}
	current, err := CurrentName()
	if err == nil && current == oldName {
		if err := SetCurrentName(newName); err != nil {
			return err
		}
		p, err := Load(newPath)
		if err == nil {
			if err := SaveDefault(p); err != nil {
				return err
			}
		}
	}
	return nil
}

func Delete(name string) error {
	name, err := sanitizeName(name)
	if err != nil {
		return err
	}
	if name == FlatProfileName {
		return fmt.Errorf("%q cannot be deleted", FlatProfileName)
	}
	dir, err := ProfilesDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name+".json")
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete profile %q: %w", name, err)
	}
	current, err := CurrentName()
	if err == nil && current == name {
		profiles, _ := List()
		if len(profiles) > 0 {
			selected := profiles[0]
			if err := SetCurrentName(selected.Name); err != nil {
				return err
			}
			if err := SaveDefault(selected.Profile); err != nil {
				return err
			}
		} else {
			defaultPath, derr := DefaultPath()
			if derr == nil {
				_ = os.Remove(defaultPath)
			}
			currentPath, cerr := CurrentNamePath()
			if cerr == nil {
				_ = os.Remove(currentPath)
			}
		}
	}
	return nil
}

func Select(name string) error {
	np, err := LoadNamed(name)
	if err != nil {
		return err
	}
	if err := SetCurrentName(np.Name); err != nil {
		return err
	}
	return SaveDefault(np.Profile)
}

func FlatProfile() *Profile {
	zeros := make([]float64, len(DefaultFrequenciesHz))
	return &Profile{
		Version:             Version,
		CreatedAt:           time.Unix(0, 0).UTC(),
		FrequenciesHz:       append([]float64(nil), DefaultFrequenciesHz...),
		LeftThresholdsDBFS:  zeros,
		RightThresholdsDBFS: append([]float64(nil), zeros...),
	}
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

func sanitizeName(name string) (string, error) {
	name = bytesTrimSpace([]byte(name))
	if name == "" {
		return "", errors.New("profile name must not be empty")
	}
	clean := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == ' ':
			clean = append(clean, r)
		default:
			clean = append(clean, '-')
		}
	}
	result := string(clean)
	for len(result) > 0 && result[0] == ' ' {
		result = result[1:]
	}
	for len(result) > 0 && result[len(result)-1] == ' ' {
		result = result[:len(result)-1]
	}
	if result == "" {
		return "", errors.New("profile name must not be empty")
	}
	return result, nil
}

func bytesTrimSpace(b []byte) string {
	start := 0
	for start < len(b) && (b[start] == ' ' || b[start] == '\n' || b[start] == '\t' || b[start] == '\r') {
		start++
	}
	end := len(b)
	for end > start && (b[end-1] == ' ' || b[end-1] == '\n' || b[end-1] == '\t' || b[end-1] == '\r') {
		end--
	}
	return string(b[start:end])
}
