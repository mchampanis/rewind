package vad

import (
	"math"
	"testing"
)

func TestStereoToMono(t *testing.T) {
	// Stereo: L=1.0, R=0.0, L=0.5, R=0.5
	stereo := []float32{1.0, 0.0, 0.5, 0.5}
	mono := stereoToMono(stereo, 2)
	if len(mono) != 2 {
		t.Fatalf("expected 2 mono samples, got %d", len(mono))
	}
	if mono[0] != 0.5 {
		t.Errorf("expected 0.5, got %f", mono[0])
	}
	if mono[1] != 0.5 {
		t.Errorf("expected 0.5, got %f", mono[1])
	}
}

func TestStereoToMonoPassthrough(t *testing.T) {
	mono := []float32{0.1, 0.2, 0.3}
	result := stereoToMono(mono, 1)
	if len(result) != 3 {
		t.Fatalf("expected passthrough for mono input")
	}
}

func TestDownsample(t *testing.T) {
	// 6 samples at 48kHz -> 2 samples at 16kHz (3:1 ratio)
	input := []float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6}
	result := downsample(input, 48000, 16000)
	if len(result) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(result))
	}
	// First group: avg(0.1, 0.2, 0.3) = 0.2
	if math.Abs(float64(result[0]-0.2)) > 1e-6 {
		t.Errorf("expected ~0.2, got %f", result[0])
	}
	// Second group: avg(0.4, 0.5, 0.6) = 0.5
	if math.Abs(float64(result[1]-0.5)) > 1e-6 {
		t.Errorf("expected ~0.5, got %f", result[1])
	}
}

func TestDownsamplePassthrough(t *testing.T) {
	input := []float32{0.1, 0.2}
	result := downsample(input, 16000, 16000)
	if len(result) != 2 {
		t.Fatalf("expected passthrough for same rate")
	}
}

func TestBuildSegments(t *testing.T) {
	// Use 100ms frames with a large gap between voiced regions.
	// Frames 0-1 voiced, frames 10-11 voiced (gap = 800ms after padding).
	frameDuration := 0.1
	probs := make([]float64, 15)
	probs[0] = 0.8
	probs[1] = 0.9
	probs[10] = 0.8
	probs[11] = 0.7
	segments := buildSegments(probs, frameDuration, 0.5)

	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}

	// First segment near the start.
	if segments[0].Start != 0 {
		t.Errorf("segment 0 start: expected 0, got %f", segments[0].Start)
	}
	// Second segment near frame 10 (1.0s - padding).
	if segments[1].Start < 0.8 {
		t.Errorf("segment 1 start too early: %f", segments[1].Start)
	}
}

func TestBuildSegmentsMerge(t *testing.T) {
	// Two voiced regions separated by a small gap (< mergeGap)
	frameDuration := 0.032
	probs := []float64{0.8, 0.8, 0.1, 0.8, 0.8}
	segments := buildSegments(probs, frameDuration, 0.5)

	// After padding and merging, these should become one segment because
	// the gap between them is small.
	if len(segments) != 1 {
		t.Fatalf("expected 1 merged segment, got %d", len(segments))
	}
}

func TestBuildSegmentsEmpty(t *testing.T) {
	probs := []float64{0.1, 0.2, 0.1, 0.3}
	segments := buildSegments(probs, 0.032, 0.5)
	if len(segments) != 0 {
		t.Fatalf("expected 0 segments for no voice, got %d", len(segments))
	}
}

func TestTrimToSegments(t *testing.T) {
	// 2 seconds of stereo audio at 4Hz (for easy math): 16 samples
	sampleRate := 4
	channels := 2
	samples := make([]float32, 16) // 2 seconds
	for i := range samples {
		samples[i] = float32(i)
	}

	// Trim to 0.5s-1.0s
	segments := []Segment{{Start: 0.5, End: 1.0}}
	trimmed := TrimToSegments(samples, sampleRate, channels, segments)

	// 0.5s at 4Hz = frame 2, 1.0s at 4Hz = frame 4
	// Frames 2-3: samples[4:8] = {4, 5, 6, 7}
	if len(trimmed) != 4 {
		t.Fatalf("expected 4 samples, got %d", len(trimmed))
	}
	if trimmed[0] != 4 || trimmed[3] != 7 {
		t.Errorf("unexpected trimmed content: %v", trimmed)
	}
}
