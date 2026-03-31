package dsp

// Speed changes playback speed by the given factor via resampling.
// factor > 1 = faster (shorter), factor < 1 = slower (longer).
func Speed(samples []float32, channels int, factor float64) []float32 {
	return resample(samples, channels, factor)
}
