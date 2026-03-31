package dsp

import (
	"fmt"
	"math/rand"
	"path/filepath"

	"rewind/internal/config"
)

// Sample loads a WAV file and mixes it into the audio according to placement.
// Relative paths are resolved against the configured samples directory.
func Sample(samples []float32, sampleRate, channels int, path, placement string) ([]float32, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(config.SamplesDir(), path)
	}

	wav, wavSR, wavCh, err := ReadWAV(path)
	if err != nil {
		return nil, fmt.Errorf("load sample %q: %w", path, err)
	}

	// Resample to match target sample rate.
	if wavSR != sampleRate {
		ratio := float64(wavSR) / float64(sampleRate)
		wav = resample(wav, wavCh, ratio)
	}

	// Match channel count.
	if wavCh != channels {
		wav = convertChannels(wav, wavCh, channels)
	}

	switch placement {
	case "start":
		out := make([]float32, len(wav)+len(samples))
		copy(out, wav)
		copy(out[len(wav):], samples)
		return out, nil

	case "end":
		out := make([]float32, len(samples)+len(wav))
		copy(out, samples)
		copy(out[len(samples):], wav)
		return out, nil

	case "overlay":
		return overlay(samples, wav, 0), nil

	case "random":
		maxStart := len(samples)/channels - len(wav)/channels
		if maxStart <= 0 {
			return overlay(samples, wav, 0), nil
		}
		startFrame := rand.Intn(maxStart)
		return overlay(samples, wav, startFrame*channels), nil

	default:
		return nil, fmt.Errorf("unknown sample placement: %q", placement)
	}
}

// overlay mixes wav into samples starting at the given sample offset.
func overlay(samples, wav []float32, offset int) []float32 {
	out := make([]float32, len(samples))
	copy(out, samples)
	for i := range wav {
		if offset+i >= len(out) {
			break
		}
		out[offset+i] += wav[i]
	}
	clamp(out)
	return out
}
