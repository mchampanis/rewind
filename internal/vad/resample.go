package vad

// stereoToMono converts interleaved stereo samples to mono by averaging L+R.
func stereoToMono(samples []float32, channels int) []float32 {
	if channels == 1 {
		return samples
	}
	frames := len(samples) / channels
	mono := make([]float32, frames)
	for i := range frames {
		var sum float32
		for ch := range channels {
			sum += samples[i*channels+ch]
		}
		mono[i] = sum / float32(channels)
	}
	return mono
}

// downsample reduces sample rate by averaging groups of samples.
// Only works for integer ratios (e.g. 48000 -> 16000 = 3:1).
func downsample(mono []float32, fromRate, toRate int) []float32 {
	if fromRate == toRate {
		return mono
	}
	ratio := fromRate / toRate
	if ratio < 1 {
		return mono
	}
	outLen := len(mono) / ratio
	out := make([]float32, outLen)
	for i := range outLen {
		var sum float32
		for j := range ratio {
			sum += mono[i*ratio+j]
		}
		out[i] = sum / float32(ratio)
	}
	return out
}
