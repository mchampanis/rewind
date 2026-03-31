package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const configFilename = "config.toml"

type Config struct {
	Daemon  DaemonConfig  `toml:"daemon"`
	Buffer  BufferConfig  `toml:"buffer"`
	Device  DeviceConfig  `toml:"device"`
	Yassify YassifyConfig `toml:"yassify"`
}

type DaemonConfig struct {
	Autostart bool `toml:"autostart"`
}

type BufferConfig struct {
	// Duration of the rolling buffer in seconds (0-180).
	Duration int `toml:"duration"`
	// Sample rate in Hz.
	SampleRate int `toml:"sample_rate"`
	// Number of audio channels.
	Channels int `toml:"channels"`
}

type DeviceConfig struct {
	// Capture is the output device to loopback-capture from (e.g. "Voicemeeter VAIO").
	Capture string `toml:"capture"`
	// Playback is the input device to render audio into (e.g. "Voicemeeter AUX VAIO").
	Playback string `toml:"playback"`
}

type YassifyConfig struct {
	// Presets maps preset names to ordered lists of effect names.
	Presets map[string]PresetConfig `toml:"presets"`
	// Effects defines parameters for built-in DSP effects.
	Effects map[string]EffectConfig `toml:"effects"`
}

type PresetConfig struct {
	Effects []string `toml:"effects"`
}

type EffectConfig struct {
	// Type is "dsp" for built-in DSP or "sample" for WAV overlay.
	Type string `toml:"type"`

	// Sample fields
	Path      string `toml:"path,omitempty"`
	Placement string `toml:"placement,omitempty"` // start, end, random, overlay

	// DSP fields
	Filter  string  `toml:"filter,omitempty"`   // bandpass, lowpass, highpass
	LowHz   int     `toml:"low_hz,omitempty"`
	HighHz  int     `toml:"high_hz,omitempty"`
	DelayMs int     `toml:"delay_ms,omitempty"`
	Decay   float64 `toml:"decay,omitempty"`
	Reverb  bool    `toml:"reverb,omitempty"` // multi-tap reverb instead of simple echo
	Gain    float64 `toml:"gain,omitempty"`
	Shift   float64 `toml:"shift,omitempty"` // pitch shift semitones
	Speed   float64 `toml:"speed,omitempty"` // playback speed multiplier
}

func defaultConfig() Config {
	return Config{
		Daemon: DaemonConfig{Autostart: false},
		Buffer: BufferConfig{
			Duration:   60,
			SampleRate: 48000,
			Channels:   2,
		},
		Device: DeviceConfig{
			Capture:  "",
			Playback: "",
		},
		Yassify: YassifyConfig{
			Presets: map[string]PresetConfig{
				"airhorn": {Effects: []string{"airhorn"}},
				"radio":   {Effects: []string{"radio", "echo"}},
				"remix":   {Effects: []string{"scratch", "reverb"}},
			},
			Effects: map[string]EffectConfig{
				"airhorn": {
					Type:      "sample",
					Path:      "airhorn.wav",
					Placement: "end",
				},
				"echo": {
					Type:    "dsp",
					DelayMs: 200,
					Decay:   0.4,
				},
				"radio": {
					Type:   "dsp",
					Filter: "bandpass",
					LowHz:  300,
					HighHz: 3400,
				},
				"scratch": {
					Type: "scratch",
				},
				"reverb": {
					Type:    "dsp",
					DelayMs: 40,
					Decay:   0.65,
					Reverb:  true,
				},
			},
		},
	}
}

// Dir returns the config directory (%APPDATA%\rewind).
func Dir() string {
	return filepath.Join(os.Getenv("APPDATA"), "rewind")
}

// Path returns the full path to the config file.
func Path() string {
	return filepath.Join(Dir(), configFilename)
}

// EffectsDir returns the directory for Lua effect plugins.
func EffectsDir() string {
	return filepath.Join(Dir(), "effects")
}

// SamplesDir returns the directory for WAV sample files.
func SamplesDir() string {
	return filepath.Join(Dir(), "samples")
}

// Load reads the config file, creating it with defaults if missing.
func Load() (*Config, error) {
	if err := os.MkdirAll(Dir(), 0755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	path := Path()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := defaultConfig()
		if err := Save(&cfg); err != nil {
			return nil, err
		}
		return &cfg, nil
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	if cfg.Buffer.Duration < 1 {
		cfg.Buffer.Duration = 1
	}
	if cfg.Buffer.Duration > 180 {
		cfg.Buffer.Duration = 180
	}
	if cfg.Buffer.SampleRate == 0 {
		cfg.Buffer.SampleRate = 48000
	}
	if cfg.Buffer.Channels == 0 {
		cfg.Buffer.Channels = 2
	}
	if cfg.Yassify.Presets == nil {
		cfg.Yassify.Presets = make(map[string]PresetConfig)
	}
	if cfg.Yassify.Effects == nil {
		cfg.Yassify.Effects = make(map[string]EffectConfig)
	}

	return &cfg, nil
}

// Save writes the config to disk atomically via write-to-temp-then-rename.
func Save(cfg *Config) error {
	dir := Dir()
	tmp, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpPath := tmp.Name()

	if err := toml.NewEncoder(tmp).Encode(cfg); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("encode config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpPath, Path()); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename config: %w", err)
	}
	return nil
}
