package buffer

import (
	"math"
	"testing"
)

func TestNewRing(t *testing.T) {
	r := New(2, 48000, 2)
	if r.Capacity() != 2.0 {
		t.Errorf("capacity = %v, want 2.0", r.Capacity())
	}
	if r.Available() != 0 {
		t.Errorf("available = %v, want 0", r.Available())
	}
}

func TestWriteAndSnapshot(t *testing.T) {
	// 1 second buffer, mono, 4 Hz (tiny for testing).
	r := New(1, 4, 1)

	// Write 4 samples (1 second).
	r.Write([]float32{1, 2, 3, 4})

	samples, dur := r.Snapshot(1.0)
	if len(samples) != 4 {
		t.Fatalf("snapshot len = %d, want 4", len(samples))
	}
	if dur != 1.0 {
		t.Errorf("duration = %v, want 1.0", dur)
	}
	for i, want := range []float32{1, 2, 3, 4} {
		if samples[i] != want {
			t.Errorf("samples[%d] = %v, want %v", i, samples[i], want)
		}
	}
}

func TestWrapAround(t *testing.T) {
	// 1 second buffer, mono, 4 Hz.
	r := New(1, 4, 1)

	// Write 6 samples -- wraps around, buffer holds last 4.
	r.Write([]float32{1, 2, 3, 4, 5, 6})

	samples, _ := r.Snapshot(1.0)
	if len(samples) != 4 {
		t.Fatalf("snapshot len = %d, want 4", len(samples))
	}
	for i, want := range []float32{3, 4, 5, 6} {
		if samples[i] != want {
			t.Errorf("samples[%d] = %v, want %v", i, samples[i], want)
		}
	}
}

func TestPartialSnapshot(t *testing.T) {
	r := New(2, 4, 1) // 2 second buffer, mono, 4 Hz = 8 samples.

	r.Write([]float32{10, 20, 30, 40, 50, 60, 70, 80})

	// Request only 0.5 seconds = 2 samples.
	samples, dur := r.Snapshot(0.5)
	if len(samples) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(samples))
	}
	if math.Abs(dur-0.5) > 0.01 {
		t.Errorf("duration = %v, want 0.5", dur)
	}
	// Should be the last 2 samples.
	for i, want := range []float32{70, 80} {
		if samples[i] != want {
			t.Errorf("samples[%d] = %v, want %v", i, samples[i], want)
		}
	}
}

func TestSnapshotMoreThanAvailable(t *testing.T) {
	r := New(2, 4, 1) // 8-sample buffer.

	// Write only 2 samples.
	r.Write([]float32{42, 43})

	// Request 2 seconds but only 0.5s available.
	samples, dur := r.Snapshot(2.0)
	if len(samples) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(samples))
	}
	if math.Abs(dur-0.5) > 0.01 {
		t.Errorf("duration = %v, want ~0.5", dur)
	}
}

func TestStereoFrameAlignment(t *testing.T) {
	r := New(1, 4, 2) // 1 second, 4 Hz, stereo = 8 samples.

	// Write 8 interleaved samples (4 frames of L,R).
	r.Write([]float32{1, 2, 3, 4, 5, 6, 7, 8})

	// Request 0.5 seconds = 2 frames = 4 samples.
	samples, _ := r.Snapshot(0.5)
	if len(samples) != 4 {
		t.Fatalf("snapshot len = %d, want 4", len(samples))
	}
	for i, want := range []float32{5, 6, 7, 8} {
		if samples[i] != want {
			t.Errorf("samples[%d] = %v, want %v", i, samples[i], want)
		}
	}
}

func TestResize(t *testing.T) {
	r := New(1, 4, 1)
	r.Write([]float32{1, 2, 3, 4})

	r.Resize(2) // grow to 2 seconds
	if r.Capacity() != 2.0 {
		t.Errorf("capacity after resize = %v, want 2.0", r.Capacity())
	}
	if r.Available() != 0 {
		t.Errorf("available after resize = %v, want 0 (data discarded)", r.Available())
	}
}

func TestIncrementalWrites(t *testing.T) {
	r := New(1, 4, 1) // 4-sample buffer.

	r.Write([]float32{1, 2})
	r.Write([]float32{3, 4})
	r.Write([]float32{5, 6}) // wraps: buffer now has [3,4,5,6]

	samples, _ := r.Snapshot(1.0)
	if len(samples) != 4 {
		t.Fatalf("snapshot len = %d, want 4", len(samples))
	}
	for i, want := range []float32{3, 4, 5, 6} {
		if samples[i] != want {
			t.Errorf("samples[%d] = %v, want %v", i, samples[i], want)
		}
	}
}

func TestEmptySnapshot(t *testing.T) {
	r := New(1, 4, 1)
	samples, dur := r.Snapshot(1.0)
	if samples != nil {
		t.Errorf("expected nil samples from empty buffer, got %v", samples)
	}
	if dur != 0 {
		t.Errorf("expected 0 duration from empty buffer, got %v", dur)
	}
}
