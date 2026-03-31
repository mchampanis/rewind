package dsp

import "math"

// Filter applies a biquad filter to interleaved audio samples.
// filterType is "lowpass", "highpass", or "bandpass".
// For bandpass, lowHz is the high-pass cutoff and highHz is the low-pass cutoff.
func Filter(samples []float32, sampleRate, channels int, filterType string, lowHz, highHz int) []float32 {
	out := make([]float32, len(samples))
	copy(out, samples)

	switch filterType {
	case "bandpass":
		if lowHz > 0 {
			applyBiquad(out, channels, highPassCoeffs(sampleRate, lowHz))
		}
		if highHz > 0 {
			applyBiquad(out, channels, lowPassCoeffs(sampleRate, highHz))
		}
	case "lowpass":
		cutoff := highHz
		if cutoff <= 0 {
			cutoff = lowHz
		}
		if cutoff > 0 {
			applyBiquad(out, channels, lowPassCoeffs(sampleRate, cutoff))
		}
	case "highpass":
		cutoff := lowHz
		if cutoff <= 0 {
			cutoff = highHz
		}
		if cutoff > 0 {
			applyBiquad(out, channels, highPassCoeffs(sampleRate, cutoff))
		}
	}

	return out
}

// biquadCoeffs holds normalized biquad filter coefficients (a0 = 1).
type biquadCoeffs struct {
	b0, b1, b2, a1, a2 float64
}

func lowPassCoeffs(sampleRate, cutoffHz int) biquadCoeffs {
	w0 := 2 * math.Pi * float64(cutoffHz) / float64(sampleRate)
	alpha := math.Sin(w0) / (2 * 0.7071) // Q = 1/sqrt(2) (Butterworth)
	cosw0 := math.Cos(w0)

	b0 := (1 - cosw0) / 2
	b1 := 1 - cosw0
	b2 := (1 - cosw0) / 2
	a0 := 1 + alpha
	a1 := -2 * cosw0
	a2 := 1 - alpha

	return biquadCoeffs{b0 / a0, b1 / a0, b2 / a0, a1 / a0, a2 / a0}
}

func highPassCoeffs(sampleRate, cutoffHz int) biquadCoeffs {
	w0 := 2 * math.Pi * float64(cutoffHz) / float64(sampleRate)
	alpha := math.Sin(w0) / (2 * 0.7071)
	cosw0 := math.Cos(w0)

	b0 := (1 + cosw0) / 2
	b1 := -(1 + cosw0)
	b2 := (1 + cosw0) / 2
	a0 := 1 + alpha
	a1 := -2 * cosw0
	a2 := 1 - alpha

	return biquadCoeffs{b0 / a0, b1 / a0, b2 / a0, a1 / a0, a2 / a0}
}

// applyBiquad processes interleaved samples in-place, one channel at a time.
func applyBiquad(samples []float32, channels int, c biquadCoeffs) {
	for ch := range channels {
		var x1, x2, y1, y2 float64
		for i := ch; i < len(samples); i += channels {
			x0 := float64(samples[i])
			y0 := c.b0*x0 + c.b1*x1 + c.b2*x2 - c.a1*y1 - c.a2*y2
			samples[i] = float32(y0)
			x2, x1 = x1, x0
			y2, y1 = y1, y0
		}
	}
}
