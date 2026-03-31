package dsp

import "math"

// Pitch shifts the audio by the given number of semitones via resampling.
// Positive = higher pitch (shorter), negative = lower pitch (longer).
func Pitch(samples []float32, channels int, semitones float64) []float32 {
	ratio := math.Pow(2, semitones/12.0)
	return resample(samples, channels, ratio)
}
