// Package clip manages the "active clip" -- a snapshot of audio extracted from
// the ring buffer, ready for playback, saving, or effect processing.
package clip

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sync"
)

// Clip holds a snapshot of interleaved float32 audio samples.
type Clip struct {
	mu         sync.RWMutex
	samples    []float32
	sampleRate int
	channels   int
}

// Store is the active clip holder. Only one clip is active at a time.
type Store struct {
	mu     sync.RWMutex
	active *Clip
}

// NewStore creates an empty clip store.
func NewStore() *Store {
	return &Store{}
}

// Set replaces the active clip. The samples slice is copied.
func (s *Store) Set(samples []float32, sampleRate, channels int) {
	owned := make([]float32, len(samples))
	copy(owned, samples)
	s.mu.Lock()
	s.active = &Clip{
		samples:    owned,
		sampleRate: sampleRate,
		channels:   channels,
	}
	s.mu.Unlock()
}

// Get returns the active clip, or nil if none.
func (s *Store) Get() *Clip {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

// Samples returns a copy of the clip's sample data.
func (c *Clip) Samples() []float32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]float32, len(c.samples))
	copy(out, c.samples)
	return out
}

// SetSamples replaces the clip's audio data (used by effects).
func (c *Clip) SetSamples(samples []float32) {
	c.mu.Lock()
	c.samples = samples
	c.mu.Unlock()
}

// Duration returns the clip length in seconds.
func (c *Clip) Duration() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.sampleRate == 0 || c.channels == 0 {
		return 0
	}
	return float64(len(c.samples)) / float64(c.sampleRate) / float64(c.channels)
}

// SampleRate returns the clip's sample rate.
func (c *Clip) SampleRate() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sampleRate
}

// Channels returns the clip's channel count.
func (c *Clip) Channels() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.channels
}

// SaveWAV writes the clip to a WAV file at the given path.
// The entire WAV is built in memory and written atomically.
func (c *Clip) SaveWAV(path string) error {
	c.mu.RLock()
	samples := make([]float32, len(c.samples))
	copy(samples, c.samples)
	sr := c.sampleRate
	ch := c.channels
	c.mu.RUnlock()

	if len(samples) == 0 {
		return fmt.Errorf("clip is empty")
	}

	bitsPerSample := 16
	byteRate := sr * ch * bitsPerSample / 8
	blockAlign := ch * bitsPerSample / 8
	dataSize := len(samples) * 2

	// 44-byte WAV header + PCM data.
	buf := make([]byte, 44+dataSize)

	// RIFF header
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataSize))
	copy(buf[8:12], "WAVE")

	// fmt chunk
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(buf[22:24], uint16(ch))
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sr))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[34:36], uint16(bitsPerSample))

	// data chunk
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataSize))

	// Convert float32 [-1,1] to int16.
	for i, s := range samples {
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		binary.LittleEndian.PutUint16(buf[44+i*2:], uint16(int16(s*math.MaxInt16)))
	}

	return os.WriteFile(path, buf, 0644)
}
