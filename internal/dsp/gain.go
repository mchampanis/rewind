package dsp

// Gain multiplies every sample by the given factor.
func Gain(samples []float32, factor float64) []float32 {
	f := float32(factor)
	out := make([]float32, len(samples))
	for i, s := range samples {
		out[i] = s * f
	}
	clamp(out)
	return out
}
