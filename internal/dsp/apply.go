package dsp

import (
	"fmt"
	"path/filepath"

	"rewind/internal/config"
)

// Apply runs an ordered effect chain on audio samples. Each effect is looked
// up by name in the effects map and applied in sequence.
func Apply(samples []float32, sampleRate, channels int, chain []string, effects map[string]config.EffectConfig) ([]float32, error) {
	for _, name := range chain {
		cfg, ok := effects[name]
		if !ok {
			return nil, fmt.Errorf("unknown effect: %q", name)
		}
		var err error
		samples, err = applyOne(samples, sampleRate, channels, cfg)
		if err != nil {
			return nil, fmt.Errorf("effect %q: %w", name, err)
		}
	}
	return samples, nil
}

func applyOne(samples []float32, sr, ch int, cfg config.EffectConfig) ([]float32, error) {
	switch cfg.Type {
	case "dsp":
		return applyDSP(samples, sr, ch, cfg), nil
	case "sample":
		return Sample(samples, sr, ch, cfg.Path, cfg.Placement)
	case "scratch":
		return Scratch(samples, sr, ch), nil
	case "lua":
		scriptPath := filepath.Join(config.EffectsDir(), cfg.Path)
		return RunLuaEffect(samples, sr, ch, scriptPath, cfg)
	default:
		return nil, fmt.Errorf("unknown effect type: %q", cfg.Type)
	}
}

// applyDSP applies all DSP operations configured in a single effect entry.
// Multiple fields can be set to stack operations (e.g. filter + gain).
func applyDSP(samples []float32, sr, ch int, cfg config.EffectConfig) []float32 {
	if cfg.Gain != 0 && cfg.Gain != 1.0 {
		samples = Gain(samples, cfg.Gain)
	}
	if cfg.Filter != "" {
		samples = Filter(samples, sr, ch, cfg.Filter, cfg.LowHz, cfg.HighHz)
	}
	if cfg.DelayMs > 0 && cfg.Reverb {
		samples = Reverb(samples, sr, ch, cfg.DelayMs, cfg.Decay)
	} else if cfg.DelayMs > 0 {
		samples = Echo(samples, sr, ch, cfg.DelayMs, cfg.Decay)
	}
	if cfg.Shift != 0 {
		samples = Pitch(samples, ch, cfg.Shift)
	}
	if cfg.Speed != 0 && cfg.Speed != 1.0 {
		samples = Speed(samples, ch, cfg.Speed)
	}
	return samples
}
