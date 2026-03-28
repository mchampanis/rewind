// Package buffer implements a circular audio buffer stored entirely in RAM.
//
// The buffer stores interleaved float32 samples (e.g. L R L R for stereo).
// A single writer (the capture goroutine) appends frames while readers
// (command handlers) can snapshot arbitrary durations without blocking the writer
// for more than the time it takes to copy a slice header.
package buffer

import (
	"sync"
)

// Ring is a circular buffer of interleaved float32 audio samples.
type Ring struct {
	mu   sync.Mutex
	data []float32
	// writePos is the next sample index to write at (wraps around).
	writePos int
	// written is the total number of samples ever written (monotonic).
	written int64
	// channels is the number of interleaved channels (e.g. 2 for stereo).
	channels int
	// sampleRate in Hz.
	sampleRate int
}

// New creates a ring buffer that holds the given duration of audio.
// duration is in seconds, sampleRate in Hz, channels is the interleave count.
func New(duration int, sampleRate int, channels int) *Ring {
	size := duration * sampleRate * channels
	if size <= 0 {
		size = 1 // degenerate case: buffer disabled, still need valid slice
	}
	return &Ring{
		data:       make([]float32, size),
		channels:   channels,
		sampleRate: sampleRate,
	}
}

// Write appends interleaved samples to the ring buffer.
// This is the hot path called by the capture goroutine.
func (r *Ring) Write(samples []float32) {
	r.mu.Lock()
	n := len(samples)
	size := len(r.data)

	if n >= size {
		// More data than buffer can hold; keep only the tail.
		copy(r.data, samples[n-size:])
		r.writePos = 0
		r.written += int64(n)
		r.mu.Unlock()
		return
	}

	// First chunk: from writePos to end of buffer (or end of samples).
	first := size - r.writePos
	if first > n {
		first = n
	}
	copy(r.data[r.writePos:], samples[:first])

	// Wrap-around chunk.
	if first < n {
		copy(r.data, samples[first:])
	}

	r.writePos = (r.writePos + n) % size
	r.written += int64(n)
	r.mu.Unlock()
}

// Snapshot copies the last `seconds` of audio from the ring buffer.
// Returns the copied samples (interleaved) and the actual duration captured.
// If less data is available than requested, returns what exists.
func (r *Ring) Snapshot(seconds float64) ([]float32, float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	size := len(r.data)
	requested := int(seconds * float64(r.sampleRate) * float64(r.channels))
	if requested > size {
		requested = size
	}

	// How many samples are actually available?
	available := int(r.written)
	if available > size {
		available = size
	}
	if requested > available {
		requested = available
	}
	if requested <= 0 {
		return nil, 0
	}

	// Align to frame boundary (channels).
	requested = (requested / r.channels) * r.channels

	out := make([]float32, requested)

	// Start position in the ring.
	start := (r.writePos - requested + size) % size

	// First chunk.
	first := size - start
	if first > requested {
		first = requested
	}
	copy(out, r.data[start:start+first])

	// Wrap-around chunk.
	if first < requested {
		copy(out[first:], r.data[:requested-first])
	}

	actualSeconds := float64(requested) / float64(r.sampleRate) / float64(r.channels)
	return out, actualSeconds
}

// Available returns the number of seconds of audio currently in the buffer.
func (r *Ring) Available() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	size := len(r.data)
	available := int(r.written)
	if available > size {
		available = size
	}
	return float64(available) / float64(r.sampleRate) / float64(r.channels)
}

// Capacity returns the total buffer duration in seconds.
func (r *Ring) Capacity() float64 {
	r.mu.Lock()
	size := len(r.data)
	r.mu.Unlock()
	return float64(size) / float64(r.sampleRate) / float64(r.channels)
}

// SampleRate returns the buffer's sample rate.
func (r *Ring) SampleRate() int {
	return r.sampleRate
}

// Channels returns the number of interleaved channels.
func (r *Ring) Channels() int {
	return r.channels
}

// Resize replaces the buffer with a new one of the given duration.
// All existing audio data is discarded.
func (r *Ring) Resize(duration int) {
	size := duration * r.sampleRate * r.channels
	if size <= 0 {
		size = 1
	}
	r.mu.Lock()
	r.data = make([]float32, size)
	r.writePos = 0
	r.written = 0
	r.mu.Unlock()
}
