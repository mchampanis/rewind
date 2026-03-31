// Package dsp implements built-in audio effects for the yassify pipeline.
package dsp

// clamp restricts all samples to the [-1, 1] range.
func clamp(samples []float32) {
	for i := range samples {
		if samples[i] > 1.0 {
			samples[i] = 1.0
		} else if samples[i] < -1.0 {
			samples[i] = -1.0
		}
	}
}

// resample changes the effective playback rate by the given ratio using
// linear interpolation. ratio > 1 = shorter output, ratio < 1 = longer.
func resample(samples []float32, channels int, ratio float64) []float32 {
	if ratio <= 0 {
		return samples
	}

	srcFrames := len(samples) / channels
	dstFrames := int(float64(srcFrames) / ratio)
	if dstFrames <= 0 {
		return nil
	}

	out := make([]float32, dstFrames*channels)

	for i := 0; i < dstFrames; i++ {
		srcPos := float64(i) * ratio
		srcIdx := int(srcPos)
		frac := float32(srcPos - float64(srcIdx))

		for ch := 0; ch < channels; ch++ {
			idx0 := srcIdx*channels + ch
			idx1 := (srcIdx+1)*channels + ch

			var s0, s1 float32
			if idx0 < len(samples) {
				s0 = samples[idx0]
			}
			if idx1 < len(samples) {
				s1 = samples[idx1]
			}

			out[i*channels+ch] = s0 + frac*(s1-s0)
		}
	}

	return out
}

// extractChannel pulls a single channel out of interleaved samples.
func extractChannel(samples []float32, channels, ch int) []float32 {
	n := len(samples) / channels
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = samples[i*channels+ch]
	}
	return out
}

// insertChannel writes mono samples back into an interleaved buffer.
func insertChannel(samples, mono []float32, channels, ch int) {
	n := len(mono)
	if n > len(samples)/channels {
		n = len(samples) / channels
	}
	for i := 0; i < n; i++ {
		samples[i*channels+ch] = mono[i]
	}
}

// convertChannels converts between different channel counts.
func convertChannels(samples []float32, fromCh, toCh int) []float32 {
	if fromCh == toCh {
		return samples
	}
	srcFrames := len(samples) / fromCh
	out := make([]float32, srcFrames*toCh)

	switch {
	case fromCh == 1 && toCh == 2:
		for i := 0; i < srcFrames; i++ {
			out[i*2] = samples[i]
			out[i*2+1] = samples[i]
		}
	case fromCh == 2 && toCh == 1:
		for i := 0; i < srcFrames; i++ {
			out[i] = (samples[i*2] + samples[i*2+1]) / 2
		}
	default:
		for i := 0; i < srcFrames; i++ {
			for ch := 0; ch < toCh; ch++ {
				out[i*toCh+ch] = samples[i*fromCh+ch%fromCh]
			}
		}
	}

	return out
}

// writeInterpolatedFrame writes an interpolated frame from src at a fractional
// position into dst at the given sample offset.
func writeInterpolatedFrame(dst []float32, dstOffset int, src []float32, srcPos float64, channels int) {
	srcFrames := len(src) / channels
	if srcFrames < 2 {
		return
	}

	if srcPos < 0 {
		srcPos = 0
	}
	if srcPos >= float64(srcFrames-1) {
		srcPos = float64(srcFrames - 2)
	}

	idx := int(srcPos)
	frac := float32(srcPos - float64(idx))

	for ch := 0; ch < channels; ch++ {
		if dstOffset+ch >= len(dst) {
			break
		}
		s0 := src[idx*channels+ch]
		s1 := src[(idx+1)*channels+ch]
		dst[dstOffset+ch] = s0 + frac*(s1-s0)
	}
}
