package dsp

import "math"

// Echo applies a single-tap delay-line echo with feedback.
// delayMs is the echo delay in milliseconds, decay is the feedback factor (0-1).
// The output is extended so the echo tail rings out to -60dB.
func Echo(samples []float32, sampleRate, channels, delayMs int, decay float64) []float32 {
	delaySamples := delayMs * sampleRate * channels / 1000
	// Align to frame boundary.
	delaySamples = (delaySamples / channels) * channels
	if delaySamples <= 0 || decay <= 0 {
		return samples
	}

	// Calculate tail: number of echo taps until energy drops below -60dB.
	maxTail := 5 * sampleRate * channels
	var tailSamples int
	absDecay := math.Abs(decay)
	if absDecay >= 1.0 {
		tailSamples = maxTail
	} else {
		taps := int(math.Ceil(math.Log(0.001) / math.Log(absDecay)))
		tailSamples = taps * delaySamples
		if tailSamples > maxTail {
			tailSamples = maxTail
		}
	}

	d := float32(decay)
	out := make([]float32, len(samples)+tailSamples)
	copy(out, samples)

	for i := delaySamples; i < len(out); i++ {
		out[i] += d * out[i-delaySamples]
	}

	clamp(out)
	return out
}
