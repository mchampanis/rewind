package dsp

// Reverb applies a Schroeder reverb using parallel comb filters followed by
// series all-pass filters. Each channel is processed independently with
// slightly different delay times to create stereo width.
// delayMs sets the base comb delay; decay controls feedback (0-1).
func Reverb(samples []float32, sampleRate, channels, delayMs int, decay float64) []float32 {
	baseDelay := delayMs * sampleRate / 1000
	if baseDelay <= 0 {
		return samples
	}

	// Extend output for the reverb tail (2 seconds).
	tailSamples := sampleRate * 2 * channels
	out := make([]float32, len(samples)+tailSamples)
	copy(out, samples)

	for ch := 0; ch < channels; ch++ {
		mono := extractChannel(out, channels, ch)

		// Scale delays per channel for stereo width. Channel 0 gets the base
		// delays; subsequent channels get progressively longer delays (~7% per
		// channel) so left and right reflections never align.
		scale := 1.0 + 0.07*float64(ch)

		combDelays := []int{
			int(float64(baseDelay) * scale),
			int(float64(baseDelay) * 1.13 * scale),
			int(float64(baseDelay) * 1.27 * scale),
			int(float64(baseDelay) * 1.49 * scale),
		}
		combOut := make([]float32, len(mono))
		for _, d := range combDelays {
			if d <= 0 {
				continue
			}
			c := combFilter(mono, d, float32(decay))
			for i := range combOut {
				combOut[i] += c[i] * 0.25
			}
		}

		// 2 series all-pass filters (also scaled per channel).
		ap1 := allPassFilter(combOut, int(float64(baseDelay)*0.31*scale), 0.5)
		ap2 := allPassFilter(ap1, int(float64(baseDelay)*0.23*scale), 0.5)

		insertChannel(out, ap2, channels, ch)
	}

	clamp(out)
	return out
}

func combFilter(in []float32, delay int, feedback float32) []float32 {
	if delay <= 0 {
		out := make([]float32, len(in))
		copy(out, in)
		return out
	}
	out := make([]float32, len(in))
	copy(out, in)
	for i := delay; i < len(out); i++ {
		out[i] += feedback * out[i-delay]
	}
	return out
}

func allPassFilter(in []float32, delay int, gain float32) []float32 {
	if delay <= 0 {
		delay = 1
	}
	out := make([]float32, len(in))
	buf := make([]float32, delay)
	pos := 0
	for i := range in {
		delayed := buf[pos]
		buf[pos] = in[i] + gain*delayed
		out[i] = delayed - gain*buf[pos]
		pos = (pos + 1) % delay
	}
	return out
}
