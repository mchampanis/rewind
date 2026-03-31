package dsp

import "math"

// Scratch generates a DJ-style scratch composition from the input clip:
//
//  1. The original clip plays through unmodified.
//  2. Quick back-and-forth scratches (3 cycles at ~5 Hz) using the last portion
//     of the clip as scratch material.
//  3. A slow-to-fast chirp scratch that ramps from ~1 Hz to ~12 Hz, like a DJ
//     winding up before a drop. A 50ms fade-out at the end prevents a hard cutoff.
//
// The output is longer than the input. Apply reverb afterward for the full
// radio-DJ / Ibiza effect.
func Scratch(samples []float32, sampleRate, channels int) []float32 {
	srcFrames := len(samples) / channels
	if srcFrames < sampleRate/2 {
		// Less than 0.5s of audio -- not enough to scratch.
		return samples
	}

	// Use the last 1.5 seconds as scratch material.
	scratchFrames := int(1.5 * float64(sampleRate))
	if scratchFrames > srcFrames {
		scratchFrames = srcFrames
	}
	scratchStart := srcFrames - scratchFrames

	// Center of the scratch region (frame index into source).
	center := float64(scratchStart + scratchFrames/2)
	amplitude := float64(scratchFrames) * 0.4

	// Section durations.
	quickDur := 0.6 // seconds -- quick back-and-forth
	rampDur := 1.5  // seconds -- slow-to-fast chirp

	quickFrames := int(quickDur * float64(sampleRate))
	rampFrames := int(rampDur * float64(sampleRate))

	totalFrames := srcFrames + quickFrames + rampFrames
	out := make([]float32, totalFrames*channels)

	// --- Section 1: original clip ---
	copy(out, samples)

	// --- Section 2: quick scratches (3 cycles at 5 Hz) ---
	quickFreq := 5.0
	for i := range quickFrames {
		t := float64(i) / float64(sampleRate)
		pos := center + amplitude*math.Sin(2*math.Pi*quickFreq*t)
		writeInterpolatedFrame(out, (srcFrames+i)*channels, samples, pos, channels)
	}

	// --- Section 3: slow-to-fast chirp scratch ---
	// Linear chirp from f0 to f1 Hz.
	f0 := 1.0
	f1 := 12.0

	// Fade-out over the last 50ms to prevent a hard cutoff.
	fadeFrames := sampleRate / 20
	if fadeFrames > rampFrames {
		fadeFrames = rampFrames
	}

	for i := range rampFrames {
		t := float64(i) / float64(sampleRate)
		tNorm := float64(i) / float64(rampFrames) // 0..1

		// Instantaneous phase of linear chirp.
		phase := 2 * math.Pi * (f0*t + (f1-f0)*t*tNorm/2)

		pos := center + amplitude*math.Sin(phase)

		dstIdx := (srcFrames + quickFrames + i) * channels
		writeInterpolatedFrame(out, dstIdx, samples, pos, channels)

		// Apply fade-out to the last 50ms.
		remaining := rampFrames - i
		if remaining < fadeFrames {
			gain := float32(float64(remaining) / float64(fadeFrames))
			for ch := range channels {
				if dstIdx+ch < len(out) {
					out[dstIdx+ch] *= gain
				}
			}
		}
	}

	return out
}
